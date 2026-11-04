package surface

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func request() Request {
	return Request{Schema, "server/routes", "routes-v1", strings.Repeat("a", 64)}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name) // #nosec G304 -- Test-selected, repository-owned fixture names; no external input.
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func build(t *testing.T, r Request, b []byte) *Snapshot {
	t.Helper()
	s, err := Build(r, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCanonicalFixtures(t *testing.T) {
	r := request()
	first := build(t, r, fixture(t, "routes.json"))
	want := bytes.TrimSuffix(fixture(t, "routes.canonical.json"), []byte("\n"))
	if !bytes.Equal(first.Canonical, want) {
		t.Fatalf("canonical bytes: %s", first.Canonical)
	}
	// Fixed external digest catches a change in encoding policy, not just order.
	if first.Digest != "b4a4a6364aab4abf100e867d217695a34141486e5912a9504972db316b07b5ab" {
		t.Fatalf("digest: %s", first.Digest)
	}
	for range 20 {
		s := build(t, r, fixture(t, "routes-reordered.json"))
		if s.Digest != first.Digest || !bytes.Equal(s.Canonical, first.Canonical) {
			t.Fatal("equivalent output changed the contract")
		}
	}
	for _, pair := range [][2]string{
		{`{"a":"\u0061","emoji":"\ud83d\ude00"}`, `{"emoji":"😀","a":"a"}`},
		{`{"a":"\/","b":"\\ud800"}`, `{"b":"\\ud800","a":"/"}`},
	} {
		a := build(t, r, []byte(`{"schema":1,"items":[{"id":"x","value":`+pair[0]+`} ]}`))
		b := build(t, r, []byte(`{"schema":1,"items":[{"id":"x","value":`+pair[1]+`} ]}`))
		if a.Digest != b.Digest {
			t.Fatal("equivalent string escaping changed digest")
		}
	}
}

func TestSemanticChanges(t *testing.T) {
	r := request()
	base := fixture(t, "routes.json")
	first := build(t, r, base)
	for name, b := range map[string][]byte{
		"method":                 fixture(t, "routes-changed.json"),
		"all removed":            fixture(t, "empty.json"),
		"array order":            []byte(strings.ReplaceAll(string(base), "[200,404]", "[404,200]")),
		"integer precision":      []byte(strings.ReplaceAll(string(base), "9007199254740993", "9007199254740992")),
		"number spelling":        []byte(strings.ReplaceAll(string(base), "200", "200.0")),
		"type":                   []byte(strings.ReplaceAll(string(base), "false", `"false"`)),
		"item identity":          []byte(strings.ReplaceAll(string(base), "route/users", "route/accounts")),
		"volatile-looking field": []byte(strings.ReplaceAll(string(base), `"required":false`, `"required":false,"timestamp":"keep me"`)),
	} {
		t.Run(name, func(t *testing.T) {
			if build(t, r, b).Digest == first.Digest {
				t.Fatal("meaningful change was lost")
			}
		})
	}
	r.InputDigest = strings.Repeat("b", 64)
	s := build(t, r, base)
	if s.Digest != first.Digest || s.InputDigest == first.InputDigest {
		t.Fatal("freshness was conflated with contract content")
	}
	r.ExtractorVersion = "routes-v2"
	if build(t, r, base).Digest == first.Digest {
		t.Fatal("extractor version was ignored")
	}
	r = request()
	r.SurfaceID = "server/other"
	if build(t, r, base).Digest == first.Digest {
		t.Fatal("surface identity was ignored")
	}
}

func TestInvalidExtraction(t *testing.T) {
	valid := fixture(t, "empty.json")
	if s := build(t, request(), valid); !bytes.Contains(s.Canonical, []byte(`"items":[]`)) {
		t.Fatal("explicit empty list lost")
	}
	failure := errors.New("extractor exited 1")
	if s, err := Build(request(), valid, failure); s != nil || !errors.Is(err, ErrExtraction) || !errors.Is(err, failure) {
		t.Fatalf("failure accepted: %+v %v", s, err)
	}
	for _, b := range [][]byte{
		nil, []byte(" \n"), []byte("null"), []byte(`{}`), []byte(`{"schema":1,"items":null}`),
		[]byte(`{"schema":2,"items":[]}`), []byte(`{"schema":1,"items":[],"extra":0}`),
		[]byte(`{"schema":1,"items":[]} {}`), []byte(`{"schema":1,"schema":1,"items":[]}`),
		fixture(t, "invalid-duplicate.json"),
		[]byte(`{"schema":1,"items":[{"id":"x","value":{}},{"id":"x","value":{}}]}`),
		[]byte(`{"schema":1,"items":[{"id":"x","value":null}]}`),
		[]byte(`{"schema":1,"items":[{"id":"x","value":{},"extra":0}]}`),
		[]byte(`{"schema":1,"items":[{"id":"x","value":{"a":0,"\u0061":1}}]}`),
		[]byte(`{"schema":1,"items":[{"id":"x","value":{"a":"\ud800"}}]}`),
		[]byte(`{"schema":1,"items":[{"id":"x","value":{"a":"\udc00"}}]}`),
		[]byte(`{"schema":1,"items":[{"id":"x","value":{"a":"\ud800\u0041"}}]}`),
		[]byte("{\"schema\":1,\"items\":[],\"\xff\":0}"),
		bytes.Repeat([]byte(" "), MaxOutputBytes+1),
		[]byte(`{"schema":1,"items":[{"id":"x","value":{"deep":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + `}}]}`),
	} {
		if s, err := Build(request(), b, nil); s != nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid output (%d bytes): %v", len(b), err)
		}
	}
}

func TestRequestContract(t *testing.T) {
	r := request()
	b, err := RequestBytes(r)
	want := `{"schema":1,"surface_id":"server/routes","extractor_version":"routes-v1","input_digest":"` + strings.Repeat("a", 64) + `"}`
	if err != nil || string(b) != want {
		t.Fatalf("request: %s %v", b, err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Schema = 0 }, func(r *Request) { r.SurfaceID = "" },
		func(r *Request) { r.ExtractorVersion = "" }, func(r *Request) { r.InputDigest = "not a digest" },
	} {
		r := request()
		mutate(&r)
		if _, err := RequestBytes(r); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid request accepted")
		}
		if s, err := Build(r, []byte(`{"schema":1,"items":[]}`), nil); s != nil || !errors.Is(err, ErrInvalid) {
			t.Fatal("build skipped request validation")
		}
	}
}

func FuzzBuild(f *testing.F) {
	f.Add([]byte(`{"schema":1,"items":[]}`))
	f.Add([]byte(`{"schema":1,"items":[{"id":"x","value":{"v":"\ud800"}}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Build(request(), b, nil)
		if err != nil {
			if s != nil {
				t.Fatal("partial snapshot on failure")
			}
			return
		}
		again := build(t, request(), b)
		if s.Digest != again.Digest {
			t.Fatal("unstable digest")
		}
	})
}
