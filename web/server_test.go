package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildH264TranscodeArgs(t *testing.T) {
	key := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	args := buildH264TranscodeArgs("http://127.0.0.1/playlist.m3u8", key, "12.5", true)

	if !slices.Contains(args, "-decryption_key") || !slices.Contains(args, "000102030405060708090a0b0c0d0e0f") {
		t.Fatalf("expected decryption key flags in args, got %v", args)
	}
	if !slices.Contains(args, "-allowed_extensions") {
		t.Fatalf("expected HLS flags in args, got %v", args)
	}
	if !slices.Contains(args, "-ss") || !slices.Contains(args, "12.50") {
		t.Fatalf("expected seek flag -ss 12.50 in args, got %v", args)
	}
	if args[len(args)-1] != "pipe:1" {
		t.Fatalf("expected pipe:1 output, got %v", args)
	}

	plainArgs := buildH264TranscodeArgs("http://127.0.0.1/video.mp4", nil, "0.2", false)
	if slices.Contains(plainArgs, "-decryption_key") {
		t.Fatalf("did not expect decryption key for unencrypted mp4: %v", plainArgs)
	}
	if slices.Contains(plainArgs, "-allowed_extensions") {
		t.Fatalf("did not expect HLS flags for mp4: %v", plainArgs)
	}
	if slices.Contains(plainArgs, "-ss") {
		t.Fatalf("did not expect -ss for start <= 0.5: %v", plainArgs)
	}
}

func TestAvailableSourceSites(t *testing.T) {
	sites := availableSourceSites()
	if len(sites) != 11 {
		t.Fatalf("expected 11 built-in sources, got %d", len(sites))
	}
	for _, s := range sites {
		if s.ID == "" || s.Name == "" {
			t.Fatalf("invalid source entry: %+v", s)
		}
		if s.Restricted {
			t.Fatalf("expected source %q not to be marked restricted", s.ID)
		}
	}
}

func TestFindFFmpegBinary(t *testing.T) {
	dummy := filepath.Join(t.TempDir(), "ffmpeg-custom")
	if err := os.WriteFile(dummy, []byte("stub"), 0600); err != nil {
		t.Fatalf("write dummy ffmpeg: %v", err)
	}
	t.Setenv("FFMPEG_PATH", dummy)
	if got := findFFmpegBinary(); got != dummy {
		t.Fatalf("expected FFMPEG_PATH %q, got %q", dummy, got)
	}

	t.Setenv("FFMPEG_PATH", "")
	if origWD, err := os.Getwd(); err == nil {
		parent := filepath.Dir(origWD)
		if _, err := os.Stat(filepath.Join(parent, "bin", "ffmpeg.exe")); err == nil {
			if err := os.Chdir(parent); err == nil {
				t.Cleanup(func() { _ = os.Chdir(origWD) })
				if got := findFFmpegBinary(); !strings.HasSuffix(strings.ToLower(got), "ffmpeg.exe") && !strings.HasSuffix(strings.ToLower(got), "ffmpeg") {
					t.Fatalf("expected repo bin/ffmpeg.exe when cwd is repo root, got %q", got)
				}
			}
		}
	}
}

func TestParseSingleHTTPRangeAndCENCDecryptServe(t *testing.T) {
	start, end, partial, err := parseSingleHTTPRange("bytes=10-19", 100)
	if err != nil || !partial || start != 10 || end != 19 {
		t.Fatalf("unexpected range parse result: start=%d end=%d partial=%v err=%v", start, end, partial, err)
	}
	start, end, partial, err = parseSingleHTTPRange("bytes=-15", 100)
	if err != nil || !partial || start != 85 || end != 99 {
		t.Fatalf("unexpected suffix range result: start=%d end=%d partial=%v err=%v", start, end, partial, err)
	}
	if _, _, _, err := parseSingleHTTPRange("bytes=100-110", 100); err == nil {
		t.Fatalf("expected out-of-bounds range error")
	}

	key := []byte("0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}

	plaintext := bytes.Repeat([]byte("ABCDEF0123456789"), 8) // 128 bytes
	var iv [16]byte
	iv[15] = 1
	encrypted := make([]byte, len(plaintext))
	copy(encrypted, plaintext)
	// Encrypt span [16:80] with subsamples: 4 clear + 60 protected
	span := cencSampleSpan{
		offset: 16,
		size:   64,
		iv:     iv,
		subsamples: []cencSubsample{
			{clear: 4, protected: 60},
		},
	}
	cipher.NewCTR(block, span.iv[:]).XORKeyStream(encrypted[20:80], plaintext[20:80])

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "encrypted.mp4", time.Time{}, bytes.NewReader(encrypted))
	}))
	defer upstream.Close()

	mgr := newCENCStreamManager()
	mgr.metadata["sample"] = &cencStreamMetadata{
		upstreamURL: upstream.URL + "/video.mp4",
		key:         key,
		block:       block,
		totalSize:   int64(len(plaintext)),
		spans:       []cencSampleSpan{span},
		lastUsed:    time.Now(),
	}

	// Request a slice starting in the middle of the encrypted span [25:95]
	req := httptest.NewRequest(http.MethodGet, "/api/stream/t/video.mp4", nil)
	req.Header.Set("Range", "bytes=25-94")
	rec := httptest.NewRecorder()
	mgr.serveDecrypted(rec, req, "sample", upstream.URL+"/video.mp4", key)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), plaintext[25:95]) {
		t.Fatalf("decrypted slice mismatch:\ngot:  %q\nwant: %q", rec.Body.Bytes(), plaintext[25:95])
	}
}

func newTestWebServer(t *testing.T) *webAppServer {
	t.Helper()
	diary := newWebDiary()
	store, err := openWebStore(t.TempDir(), diary)
	if err != nil {
		t.Fatalf("openWebStore: %v", err)
	}
	tempDir := filepath.Join(t.TempDir(), "tmp")
	return newWebAppServer("test", false, "", tempDir, store, diary)
}

func TestHandleSourcesCheckAndVendorHLS(t *testing.T) {
	srv := newTestWebServer(t)
	srv.sourceProbeFunc = func(ctx context.Context, sourceID string) sourceCheckResult {
		return sourceCheckResult{
			OK:        sourceID != "kuaishou",
			LatencyMs: 25,
			Count:     12,
			Error: func() string {
				if sourceID == "kuaishou" {
					return "模拟失败"
				}
				return ""
			}(),
		}
	}

	handler := srv.routes()

	// Method check
	getReq := httptest.NewRequest(http.MethodGet, "/api/sources/check", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on GET /api/sources/check, got %d", getRec.Code)
	}

	// Single source check
	singleReq := httptest.NewRequest(http.MethodPost, "/api/sources/check", strings.NewReader(`{"source":"hongguo"}`))
	singleRec := httptest.NewRecorder()
	handler.ServeHTTP(singleRec, singleReq)
	if singleRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on single source check, got %d: %s", singleRec.Code, singleRec.Body.String())
	}
	var payload struct {
		OK      bool                `json:"ok"`
		Results []sourceCheckResult `json:"results"`
	}
	if err := json.Unmarshal(singleRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || len(payload.Results) != 1 || payload.Results[0].ID != "hongguo" || !payload.Results[0].OK {
		t.Fatalf("unexpected single source check payload: %+v", payload)
	}

	// All sources check
	allReq := httptest.NewRequest(http.MethodPost, "/api/sources/check", strings.NewReader(`{}`))
	allRec := httptest.NewRecorder()
	handler.ServeHTTP(allRec, allReq)
	if allRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on all sources check, got %d", allRec.Code)
	}
	if err := json.Unmarshal(allRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode all response: %v", err)
	}
	if len(payload.Results) != 11 {
		t.Fatalf("expected 11 source check results, got %d", len(payload.Results))
	}

	// Unknown source check
	badReq := httptest.NewRequest(http.MethodPost, "/api/sources/check", strings.NewReader(`{"source":"nonexistent"}`))
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on unknown source, got %d", badRec.Code)
	}

	// Bundled local hls.min.js static route
	hlsReq := httptest.NewRequest(http.MethodGet, "/vendor/hls.min.js", nil)
	hlsRec := httptest.NewRecorder()
	handler.ServeHTTP(hlsRec, hlsReq)
	if hlsRec.Code != http.StatusOK || hlsRec.Body.Len() < 100000 {
		t.Fatalf("expected embedded /vendor/hls.min.js, got status=%d len=%d", hlsRec.Code, hlsRec.Body.Len())
	}
}

func TestBeginTranscodeSessionCancelsPreviousSameToken(t *testing.T) {
	srv := newTestWebServer(t)

	ctx1, cleanup1 := srv.beginTranscodeSession("tok-a", context.Background())
	defer cleanup1()
	ctxOther, cleanupOther := srv.beginTranscodeSession("tok-b", context.Background())
	defer cleanupOther()

	ctx2, cleanup2 := srv.beginTranscodeSession("tok-a", context.Background())
	defer cleanup2()

	select {
	case <-ctx1.Done():
	case <-time.After(time.Second):
		t.Fatalf("expected prior transcode context for tok-a to be cancelled")
	}
	if ctx2.Err() != nil {
		t.Fatalf("expected replacement context for tok-a to remain active, got %v", ctx2.Err())
	}
	if ctxOther.Err() != nil {
		t.Fatalf("expected independent context for tok-b to remain active, got %v", ctxOther.Err())
	}
}
