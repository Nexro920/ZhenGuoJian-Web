package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	huangguoEdgeProtocol    = "dx150-edge-v1"
	huangguoEdgeMaxSequence = 16384
)

var (
	reHuangguoEdgeKID  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	reHuangguoEdgeSID  = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	reHuangguoEdgeSite = regexp.MustCompile(`^[A-Za-z0-9.:\[\]-]{1,256}$`)
)

type huangguoEdgeSession struct {
	mu          sync.Mutex
	base        string
	scope       string
	info        string
	kid         string
	sid         string
	clockOffset int64
	expiresAt   int64
	sequence    uint32
	reqAEAD     cipher.AEAD
	resAEAD     cipher.AEAD
}

func huangguoEdgeScope(rawPath string) string {
	route, rawQuery, _ := strings.Cut(rawPath, "?")
	route = strings.TrimSpace(route)
	if strings.HasPrefix(route, "/api/videos/") {
		rest := strings.TrimPrefix(route, "/api/videos/")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) == 2 && (parts[1] == "play" || parts[1] == "playback") {
			return "playback"
		}
		if len(parts) == 3 && parts[1] == "episodes" && parts[2] == "playback" {
			return "playback"
		}
		if len(parts) == 4 && parts[1] == "episodes" && parts[3] == "playback" {
			return "playback"
		}
		if len(parts) == 1 && parts[0] != "" && parts[0] != "long" && parts[0] != "following" {
			return "dynamic"
		}
		if len(parts) == 2 && (parts[1] == "next" || parts[1] == "comments") {
			return "dynamic"
		}
	}
	if route == "/api/search" || route == "/api/home" || route == "/api/whoami" ||
		route == "/api/site/address-poster" || route == "/api/site/entries" || route == "/api/site/go-home" {
		return "dynamic"
	}
	if values, err := url.ParseQuery(rawQuery); err == nil {
		sortVal := strings.TrimSpace(values.Get("sort"))
		if sortVal == "random" || sortVal == "rand" {
			return "dynamic"
		}
	}
	return "public"
}

func huangguoEdgeEndpoint(base, scope string) string {
	return strings.TrimRight(base, "/") + "/api/" + scope + "/request"
}

func decodeEdgeBase64URL(value string) ([]byte, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "=")
	if trimmed == "" {
		return nil, errors.New("黄果加密字段为空")
	}
	return base64.RawURLEncoding.DecodeString(trimmed)
}

func deriveEdgeAEAD(secret, salt []byte, info string) (cipher.AEAD, error) {
	reader := hkdf.New(sha256.New, secret, salt, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

type huangguoEdgeSessionCall struct {
	done    chan struct{}
	session *huangguoEdgeSession
	err     error
}

type providerSingleHostKey struct{}

func huangguoShouldUseEdgeV1(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 400") ||
		strings.Contains(msg, "HTTP 404") ||
		strings.Contains(msg, "HTTP 405") ||
		strings.Contains(msg, "HTTP 410")
}

func (d *Downloader) huangguoEdgeMode(base string) string {
	d.edgeMu.Lock()
	defer d.edgeMu.Unlock()
	if d.edgeModes == nil {
		return ""
	}
	return d.edgeModes[base]
}

func (d *Downloader) setHuangguoEdgeMode(base, mode string) {
	d.edgeMu.Lock()
	if d.edgeModes == nil {
		d.edgeModes = map[string]string{}
	}
	d.edgeModes[base] = mode
	d.edgeMu.Unlock()
}

func (d *Downloader) huangguoEdgeGetSession(ctx context.Context, base, scope string) (*huangguoEdgeSession, error) {
	key := scope + "|" + base
	now := time.Now().Unix()
	d.edgeMu.Lock()
	if d.edgeSessions != nil {
		if s := d.edgeSessions[key]; s != nil {
			s.mu.Lock()
			valid := s.expiresAt > now+5 && s.sequence < huangguoEdgeMaxSequence
			s.mu.Unlock()
			if valid {
				d.edgeMu.Unlock()
				return s, nil
			}
		}
	}
	if d.edgePending == nil {
		d.edgePending = map[string]*huangguoEdgeSessionCall{}
	}
	if call := d.edgePending[key]; call != nil {
		d.edgeMu.Unlock()
		select {
		case <-call.done:
			return call.session, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &huangguoEdgeSessionCall{done: make(chan struct{})}
	d.edgePending[key] = call
	d.edgeMu.Unlock()

	openCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	session, err := d.huangguoEdgeOpenSession(openCtx, base, scope)
	cancel()

	d.edgeMu.Lock()
	delete(d.edgePending, key)
	if err == nil {
		if d.edgeSessions == nil {
			d.edgeSessions = map[string]*huangguoEdgeSession{}
		}
		d.edgeSessions[key] = session
	}
	call.session, call.err = session, err
	close(call.done)
	d.edgeMu.Unlock()
	return session, err
}

func (d *Downloader) huangguoEdgeInvalidateSession(base, scope string, session *huangguoEdgeSession) {
	key := scope + "|" + base
	d.edgeMu.Lock()
	if d.edgeSessions != nil && d.edgeSessions[key] == session {
		delete(d.edgeSessions, key)
	}
	d.edgeMu.Unlock()
}

func (d *Downloader) huangguoEdgeOpenSession(ctx context.Context, base, scope string) (*huangguoEdgeSession, error) {
	curve := ecdh.P256()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	helloReq, err := json.Marshal(map[string]any{
		"v":          1,
		"type":       "hello",
		"client_pub": base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
	})
	if err != nil {
		return nil, err
	}
	endpoint := huangguoEdgeEndpoint(base, scope)
	rawResp, err := d.fetchProviderRequest(ctx, endpoint, strings.TrimRight(base, "/")+"/", helloReq)
	if err != nil {
		return nil, err
	}
	var hello struct {
		V          int    `json:"v"`
		Type       string `json:"type"`
		KID        string `json:"kid"`
		SID        string `json:"sid"`
		Site       string `json:"site"`
		ServerTime int64  `json:"server_time"`
		ExpiresAt  int64  `json:"expires_at"`
		ServerPub  string `json:"server_pub"`
		Salt       string `json:"salt"`
	}
	if err := json.Unmarshal([]byte(rawResp), &hello); err != nil {
		return nil, fmt.Errorf("黄果加密握手响应无效：%w", err)
	}
	if hello.V != 1 || hello.Type != "hello" ||
		!reHuangguoEdgeKID.MatchString(hello.KID) ||
		!reHuangguoEdgeSID.MatchString(hello.SID) ||
		!reHuangguoEdgeSite.MatchString(hello.Site) ||
		hello.ExpiresAt <= hello.ServerTime || hello.ExpiresAt-hello.ServerTime > 600 {
		return nil, errors.New("黄果加密握手响应参数无效")
	}
	serverPubBytes, err := decodeEdgeBase64URL(hello.ServerPub)
	if err != nil {
		return nil, fmt.Errorf("黄果加密公钥无效：%w", err)
	}
	serverPub, err := curve.NewPublicKey(serverPubBytes)
	if err != nil {
		return nil, fmt.Errorf("黄果加密公钥格式错误：%w", err)
	}
	shared, err := priv.ECDH(serverPub)
	if err != nil {
		return nil, fmt.Errorf("黄果加密密钥协商失败：%w", err)
	}
	salt, err := decodeEdgeBase64URL(hello.Salt)
	if err != nil || len(salt) != 32 {
		return nil, errors.New("黄果加密握手盐值无效")
	}
	info := strings.Join([]string{huangguoEdgeProtocol, scope, hello.Site, hello.KID, hello.SID}, "\n")
	reqAEAD, err := deriveEdgeAEAD(shared, salt, info+"\nreq")
	if err != nil {
		return nil, err
	}
	resAEAD, err := deriveEdgeAEAD(shared, salt, info+"\nres")
	if err != nil {
		return nil, err
	}
	localTime := time.Now().Unix()
	return &huangguoEdgeSession{
		base:        base,
		scope:       scope,
		info:        info,
		kid:         hello.KID,
		sid:         hello.SID,
		clockOffset: hello.ServerTime - localTime,
		expiresAt:   localTime + (hello.ExpiresAt - hello.ServerTime),
		reqAEAD:     reqAEAD,
		resAEAD:     resAEAD,
	}, nil
}

func (s *huangguoEdgeSession) nextSequence() (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.expiresAt <= time.Now().Unix()+5 || s.sequence >= huangguoEdgeMaxSequence {
		return 0, false
	}
	s.sequence++
	return s.sequence, true
}

func (d *Downloader) huangguoEdgeCall(ctx context.Context, base, method, path string) ([]byte, error) {
	scope := huangguoEdgeScope(path)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		session, err := d.huangguoEdgeGetSession(ctx, base, scope)
		if err != nil {
			return nil, err
		}
		seq, ok := session.nextSequence()
		if !ok {
			d.huangguoEdgeInvalidateSession(base, scope, session)
			continue
		}
		ts := time.Now().Unix() + session.clockOffset
		nonce := make([]byte, 12)
		binary.BigEndian.PutUint32(nonce[8:12], seq)

		packetBytes, err := json.Marshal(map[string]any{
			"request": map[string]string{
				"method": method,
				"path":   path,
			},
		})
		if err != nil {
			return nil, err
		}
		reqAAD := []byte(fmt.Sprintf("%s\nreq\n%d\n%d", session.info, seq, ts))
		sealed := session.reqAEAD.Seal(nil, nonce, packetBytes, reqAAD)
		envelope, err := json.Marshal(map[string]any{
			"v":          1,
			"type":       "call",
			"kid":        session.kid,
			"sid":        session.sid,
			"seq":        seq,
			"ts":         ts,
			"ciphertext": base64.RawURLEncoding.EncodeToString(sealed),
		})
		if err != nil {
			return nil, err
		}
		endpoint := huangguoEdgeEndpoint(base, scope)
		rawResp, err := d.fetchProviderRequest(ctx, endpoint, strings.TrimRight(base, "/")+"/", envelope)
		if err != nil {
			d.huangguoEdgeInvalidateSession(base, scope, session)
			if ctx.Err() != nil {
				return nil, err
			}
			lastErr = err
			continue
		}
		var result struct {
			V          int    `json:"v"`
			Type       string `json:"type"`
			KID        string `json:"kid"`
			SID        string `json:"sid"`
			Seq        uint32 `json:"seq"`
			TS         int64  `json:"ts"`
			Ciphertext string `json:"ciphertext"`
		}
		if err := json.Unmarshal([]byte(rawResp), &result); err != nil {
			return nil, fmt.Errorf("黄果加密接口响应无效：%w", err)
		}
		if result.V != 1 || result.Type != "result" ||
			result.KID != session.kid || result.SID != session.sid ||
			result.Seq != seq || result.TS != ts {
			d.huangguoEdgeInvalidateSession(base, scope, session)
			lastErr = errors.New("黄果加密接口响应参数不匹配")
			continue
		}
		ctBytes, err := decodeEdgeBase64URL(result.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("黄果加密接口密文无效：%w", err)
		}
		resAAD := []byte(fmt.Sprintf("%s\nres\n%d\n%d", session.info, seq, ts))
		plaintext, err := session.resAEAD.Open(nil, nonce, ctBytes, resAAD)
		if err != nil {
			d.huangguoEdgeInvalidateSession(base, scope, session)
			lastErr = fmt.Errorf("黄果加密接口解密失败：%w", err)
			continue
		}
		var inner struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		}
		if err := json.Unmarshal(plaintext, &inner); err != nil {
			return nil, fmt.Errorf("黄果加密内层响应无效：%w", err)
		}
		var body []byte
		if inner.Body != "" {
			body, err = base64.StdEncoding.DecodeString(inner.Body)
			if err != nil {
				return nil, fmt.Errorf("黄果加密内层数据解码失败：%w", err)
			}
		}
		if inner.Status < http.StatusOK || inner.Status >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("黄果加密接口返回 HTTP %d：%s", inner.Status, truncate(cleanText(string(body)), 160))
		}
		return body, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("黄果加密接口请求失败")
}
