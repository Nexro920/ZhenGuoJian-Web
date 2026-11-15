package core

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHuangguoEdgeScopeRouting(t *testing.T) {
	cases := map[string]string{
		"/api/videos/12":                     "dynamic",
		"/api/search?q=test&page=1&size=20":  "dynamic",
		"/api/videos/12/play?ep=1":           "playback",
		"/api/videos/12/episodes/2/playback": "playback",
		"/api/videos/category/ai-duanju":     "public",
		"/api/videos/category/all?sort=rand": "dynamic",
		"/api/ranks/hot":                     "public",
	}
	for path, want := range cases {
		if got := huangguoEdgeScope(path); got != want {
			t.Fatalf("huangguoEdgeScope(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestHuangguoEdgeClientEncryptedDetailAndPlayback(t *testing.T) {
	curve := ecdh.P256()
	serverPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	const kid = "kid1"
	const sid = "session-identifier-123456"
	const site = "mock.example"

	sessions := map[string]struct {
		info string
		req  []byte
		res  []byte
	}{}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var scope string
		switch r.URL.Path {
		case "/api/dynamic/request":
			scope = "dynamic"
		case "/api/playback/request":
			scope = "playback"
		default:
			http.NotFound(w, r)
			return
		}
		var msg struct {
			V          int    `json:"v"`
			Type       string `json:"type"`
			ClientPub  string `json:"client_pub"`
			KID        string `json:"kid"`
			SID        string `json:"sid"`
			Seq        uint32 `json:"seq"`
			TS         int64  `json:"ts"`
			Ciphertext string `json:"ciphertext"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if msg.Type == "hello" {
			pubBytes, err := decodeEdgeBase64URL(msg.ClientPub)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			clientPub, err := curve.NewPublicKey(pubBytes)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			shared, err := serverPriv.ECDH(clientPub)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			info := strings.Join([]string{huangguoEdgeProtocol, scope, site, kid, sid}, "\n")
			sessions[scope] = struct {
				info string
				req  []byte
				res  []byte
			}{info: info, req: shared}
			now := time.Now().Unix()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"v":           1,
				"type":        "hello",
				"kid":         kid,
				"sid":         sid,
				"site":        site,
				"server_time": now,
				"expires_at":  now + 120,
				"server_pub":  base64.RawURLEncoding.EncodeToString(serverPriv.PublicKey().Bytes()),
				"salt":        base64.RawURLEncoding.EncodeToString(salt),
			})
			return
		}
		sess, ok := sessions[scope]
		if !ok {
			http.Error(w, "no session", http.StatusBadRequest)
			return
		}
		reqAEAD, err := deriveEdgeAEAD(sess.req, salt, sess.info+"\nreq")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resAEAD, err := deriveEdgeAEAD(sess.req, salt, sess.info+"\nres")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		nonce := make([]byte, 12)
		binary.BigEndian.PutUint32(nonce[8:12], msg.Seq)
		ct, err := decodeEdgeBase64URL(msg.Ciphertext)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqAAD := []byte(fmt.Sprintf("%s\nreq\n%d\n%d", sess.info, msg.Seq, msg.TS))
		plain, err := reqAEAD.Open(nil, nonce, ct, reqAAD)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var innerReq struct {
			Request struct {
				Method string `json:"method"`
				Path   string `json:"path"`
			} `json:"request"`
		}
		if err := json.Unmarshal(plain, &innerReq); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var payloadJSON string
		switch {
		case innerReq.Request.Method == http.MethodGet && innerReq.Request.Path == "/api/videos/12":
			payloadJSON = `{"status":1,"msg":"","data":{"id":12,"title":"加密短剧","episodes":[{"ep_num":1,"title":"第1集"}]}}`
		case innerReq.Request.Method == http.MethodPost && innerReq.Request.Path == "/api/videos/12/episodes/1/playback":
			payloadJSON = fmt.Sprintf(`{"status":1,"msg":"","data":{"streams":{"main":%q}}}`, serverURL+"/media/ep1.m3u8")
		default:
			http.Error(w, "unexpected route "+innerReq.Request.Method+" "+innerReq.Request.Path, http.StatusNotFound)
			return
		}
		innerResp, _ := json.Marshal(map[string]any{
			"status": http.StatusOK,
			"body":   base64.StdEncoding.EncodeToString([]byte(payloadJSON)),
		})
		resAAD := []byte(fmt.Sprintf("%s\nres\n%d\n%d", sess.info, msg.Seq, msg.TS))
		sealed := resAEAD.Seal(nil, nonce, innerResp, resAAD)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"v":          1,
			"type":       "result",
			"kid":        kid,
			"sid":        sid,
			"seq":        msg.Seq,
			"ts":         msg.TS,
			"ciphertext": base64.RawURLEncoding.EncodeToString(sealed),
		})
	}))
	defer server.Close()
	serverURL = server.URL

	d := &Downloader{cfg: Config{HuangguoAIURL: server.URL}, client: server.Client(), limiter: newRequestLimiter(3, 0), providerHosts: map[string]string{}}
	drama, chapters, err := d.fetchHuangguoAIDetail(context.Background(), "12")
	if err != nil {
		t.Fatalf("fetchHuangguoAIDetail failed: %v", err)
	}
	if drama.Title != "加密短剧" || len(chapters) != 1 {
		t.Fatalf("unexpected detail: title=%q chapters=%d", drama.Title, len(chapters))
	}
	playURL, err := d.fetchHuangguoAIPlayURL(context.Background(), "12", 1)
	if err != nil {
		t.Fatalf("fetchHuangguoAIPlayURL failed: %v", err)
	}
	wantURL := strings.Replace(server.URL, "http://", "https://", 1) + "/media/ep1.m3u8"
	if playURL != wantURL {
		t.Fatalf("playURL = %q, want %q", playURL, wantURL)
	}
}

func TestHanxiaoquanAlternateLineFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/play/4570-0-0.html":
			w.WriteHeader(http.StatusNotFound)
		case "/hanxiaoquan/4570.html":
			_, _ = io.WriteString(w, `
				<a href="#playlist0"><i></i>线路一</a>
				<a href="#playlist1"><i></i>线路二</a>
				<div id="playlist0">
					<a title="第1集" href="/play/4570-0-0.html"><span>第1集</span></a>
				</div>
				<div id="playlist1">
					<a title="第1集" href="/play/4570-1-0.html"><span>第1集</span></a>
				</div>
			`)
		case "/play/4570-1-0.html":
			_, _ = io.WriteString(w, `<script>var now="https://127.0.0.1/fallback/ep1.mp4";</script>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	d := &Downloader{cfg: Config{HanxiaoquanURL: server.URL}, client: server.Client(), limiter: newRequestLimiter(3, 0), providerHosts: map[string]string{}}
	media, err := d.resolveHanxiaoquanMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceHanxiaoquan, "4570"),
		Chapter: Chapter{
			ID:             providerChapterID(sourceHanxiaoquan, "4570", "/play/4570-0-0.html"),
			Source:         sourceHanxiaoquan,
			CurrentEpisode: rawEpisode(1),
			PageURL:        "/play/4570-0-0.html",
		},
	})
	if err != nil {
		t.Fatalf("resolveHanxiaoquanMedia fallback failed: %v", err)
	}
	if media.URL != "https://127.0.0.1/fallback/ep1.mp4" {
		t.Fatalf("media.URL = %q", media.URL)
	}
}

func TestSoraniCanPlayFalseAndLevelTag(t *testing.T) {
	drama, err := soraniDramaFromMap(map[string]any{
		"id":        4744,
		"name":      "示例番剧",
		"levelText": "登录可见",
	})
	if err != nil {
		t.Fatalf("soraniDramaFromMap failed: %v", err)
	}
	if len(drama.Tags) == 0 || drama.Tags[0] != "登录可见" {
		t.Fatalf("expected levelText tag, got %#v", drama.Tags)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200,"data":{"canPlay":false,"message":"请先登录","playUrl":null}}`)
	}))
	defer server.Close()

	d := &Downloader{cfg: Config{SoraniAPIURL: server.URL, SoraniURL: server.URL}, client: server.Client(), limiter: newRequestLimiter(3, 0), providerHosts: map[string]string{}}
	_, err = d.resolveSoraniMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceSorani, "4744"),
		Chapter: Chapter{ID: providerChapterID(sourceSorani, "4744", "9001"), Source: sourceSorani},
	})
	if err == nil || !strings.Contains(err.Error(), "请先登录") {
		t.Fatalf("expected login message error, got %v", err)
	}
}

func TestIsHuangdouCertErrorScoped(t *testing.T) {
	expired := x509.CertificateInvalidError{Reason: x509.Expired}
	if !isHuangdouCertError("xqjurgek.top", expired) {
		t.Fatalf("expected xqjurgek.top expired cert to match")
	}
	if isHuangdouCertError("example.com", expired) {
		t.Fatalf("unrelated host must not match")
	}
	if isHuangdouCertError("xqjurgek.top", errors.New("connection refused")) {
		t.Fatalf("non-TLS error must not match")
	}
}

func TestHuangjuCategoriesLargeList(t *testing.T) {
	rows := make([]map[string]any, 0, 380)
	for i := 1; i <= 380; i++ {
		rows = append(rows, map[string]any{
			"slug": fmt.Sprintf("cat-%d", i),
			"name": fmt.Sprintf("分类%d", i),
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth/guest" {
			_, _ = io.WriteString(w, `{"token":"test-guest-token"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
	}))
	defer server.Close()

	d := &Downloader{cfg: Config{HuangjuAPIURL: server.URL, HuangjuURL: server.URL}, client: server.Client(), limiter: newRequestLimiter(3, 0), providerHosts: map[string]string{}}
	categories, err := d.fetchHuangjuCategories(context.Background())
	if err != nil {
		t.Fatalf("fetchHuangjuCategories failed: %v", err)
	}
	if len(categories) != 381 {
		t.Fatalf("expected 381 categories (including newest), got %d", len(categories))
	}
}

func TestSoraniSearchLocalPagination(t *testing.T) {
	rows := make([]map[string]any, 0, 520)
	for i := 1; i <= 520; i++ {
		rows = append(rows, map[string]any{
			"id":   1000 + i,
			"name": fmt.Sprintf("搜索番剧%d", i),
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": rows})
	}))
	defer server.Close()

	d := &Downloader{cfg: Config{SoraniAPIURL: server.URL, SoraniURL: server.URL}, client: server.Client(), limiter: newRequestLimiter(3, 0), providerHosts: map[string]string{}}
	page1, hasMore1, err := d.fetchSoraniCatalogPage(context.Background(), 1, "", "爱")
	if err != nil {
		t.Fatalf("fetchSoraniCatalogPage page 1 failed: %v", err)
	}
	if len(page1) != 24 || !hasMore1 {
		t.Fatalf("unexpected page 1 result: len=%d hasMore=%v", len(page1), hasMore1)
	}
	pageLast, hasMoreLast, err := d.fetchSoraniCatalogPage(context.Background(), 22, "", "爱")
	if err != nil {
		t.Fatalf("fetchSoraniCatalogPage page 22 failed: %v", err)
	}
	if len(pageLast) != 16 || hasMoreLast {
		t.Fatalf("unexpected last page result: len=%d hasMore=%v", len(pageLast), hasMoreLast)
	}
}

func TestSelectHongguoAppMediaCodecTags(t *testing.T) {
	hevcOnly := map[string]any{
		"video_duration": "45",
		"video_list": []any{
			map[string]any{
				"main_url":   "https://cdn.example.test/hevc.mp4",
				"video_meta": map[string]any{"codec_type": "bytevc1", "definition": "1080p", "vheight": 1920},
			},
		},
	}
	media, err := selectHongguoAppMedia(hevcOnly)
	if err != nil {
		t.Fatalf("selectHongguoAppMedia hevcOnly failed: %v", err)
	}
	if media.Codec != "hevc" {
		t.Fatalf("expected Codec=hevc, got %q", media.Codec)
	}

	mixed := map[string]any{
		"video_list": []any{
			map[string]any{
				"main_url":   "https://cdn.example.test/hevc.mp4",
				"video_meta": map[string]any{"codec_type": "bytevc1", "definition": "720p", "vheight": 1280},
			},
			map[string]any{
				"main_url":   "https://cdn.example.test/h264.mp4",
				"video_meta": map[string]any{"codec_type": "h264", "definition": "720p", "vheight": 1280},
			},
		},
	}
	mediaMixed, err := selectHongguoAppMedia(mixed)
	if err != nil {
		t.Fatalf("selectHongguoAppMedia mixed failed: %v", err)
	}
	if mediaMixed.Codec != "h264" || mediaMixed.URL != "https://cdn.example.test/h264.mp4" {
		t.Fatalf("expected H264 variant preferred, got codec=%q url=%q", mediaMixed.Codec, mediaMixed.URL)
	}
}

func TestHuangguoAIPlaybackOptionsCodecAndFallback(t *testing.T) {
	raw := []byte(`{"status":1,"data":{"streams":{"main":"https://cdn.example.test/main.m3u8","hevc":"https://cdn.example.test/hevc.m3u8"}}}`)
	opts := huangguoAIPlaybackOptions("https://huangguoai.com", raw)
	if len(opts) != 2 {
		t.Fatalf("expected 2 playback options, got %d", len(opts))
	}
	if opts[0].URL != "https://cdn.example.test/main.m3u8" || opts[0].Codec != "" {
		t.Fatalf("unexpected primary option: %+v", opts[0])
	}
	if opts[1].URL != "https://cdn.example.test/hevc.m3u8" || opts[1].Codec != "hevc" {
		t.Fatalf("unexpected hevc fallback option: %+v", opts[1])
	}

	hevcOnly := []byte(`{"status":1,"data":{"streams":{"main":"","hevc":"https://cdn.example.test/only-hevc.m3u8"}}}`)
	optsHevc := huangguoAIPlaybackOptions("https://huangguoai.com", hevcOnly)
	if len(optsHevc) != 1 || optsHevc[0].Codec != "hevc" {
		t.Fatalf("expected single hevc option tagged, got %+v", optsHevc)
	}
}
