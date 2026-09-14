package caddy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenBoundariesAndLock(t *testing.T) {
	fixture := newFixture(t)
	if _, err := Open(fixture.options); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open got %v", err)
	}
	invalidURLs := []string{
		"", "https://127.0.0.1:2019", "http://localhost:2019", "http://10.0.0.1:2019",
		"http://127.0.0.1", "http://127.0.0.1:0", "http://user@127.0.0.1:2019",
		"http://127.0.0.1:2019/", "http://127.0.0.1:2019?x=1", "http://[::1]:2019/#x",
	}
	for _, value := range invalidURLs {
		options := fixture.options
		options.AdminURL = value
		if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("AdminURL %q got %v", value, err)
		}
	}
	for _, timeout := range []time.Duration{-time.Second, time.Millisecond, time.Minute + time.Second} {
		options := fixture.options
		options.Timeout = timeout
		if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("timeout %v got %v", timeout, err)
		}
	}
	options := fixture.options
	options.StateRoot = options.ConfigPath
	if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("overlap got %v", err)
	}
	options = fixture.options
	options.WorkingDirectory = "relative"
	if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("relative working directory got %v", err)
	}
	options.WorkingDirectory = fixture.options.WorkingDirectory + "/missing"
	if _, err := Open(options); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("missing working directory got %v", err)
	}
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.adapter.Close(); !errors.Is(err, ErrClosed) {
		t.Fatalf("second close got %v", err)
	}
	reopened, err := Open(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsUnsafeFilesystemObjects(t *testing.T) {
	t.Run("binary digest", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		options := fixture.options
		options.CaddyBinaryDigest = digest([]byte("different"))
		if _, err := Open(options); !errors.Is(err, ErrConflict) {
			t.Fatalf("got %v", err)
		}
		options.CaddyBinaryDigest = "bad"
		if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("shape got %v", err)
		}
	})
	t.Run("binary mode", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(fixture.options.CaddyBinary, 0o722); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("config mode", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(fixture.options.ConfigPath, 0o622); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("state mode", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(fixture.options.StateRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("config symlink", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		real := fixture.options.ConfigPath + ".real"
		if err := os.Rename(fixture.options.ConfigPath, real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, fixture.options.ConfigPath); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("config hard link", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(fixture.options.ConfigPath, fixture.options.ConfigPath+".link"); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("binary symlink", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		real := fixture.options.CaddyBinary + ".real"
		if err := os.Rename(fixture.options.CaddyBinary, real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, fixture.options.CaddyBinary); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("binary hard link", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(fixture.options.CaddyBinary, fixture.options.CaddyBinary+".link"); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestAdminReaderBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   []byte
		want   error
	}{
		{name: "canonical object", status: http.StatusOK, body: []byte("{\"b\":2,\"a\":1}\n")},
		{name: "status", status: http.StatusBadGateway, body: []byte("{}"), want: ErrRuntime},
		{name: "invalid", status: http.StatusOK, body: []byte("no"), want: ErrRuntime},
		{name: "trailing", status: http.StatusOK, body: []byte("{} {}"), want: ErrRuntime},
		{name: "array", status: http.StatusOK, body: []byte("[]"), want: ErrRuntime},
		{name: "large", status: http.StatusOK, body: bytes.Repeat([]byte{'x'}, MaxJSONBytes+1), want: ErrLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write(test.body)
			}))
			defer server.Close()
			reader := &adminReader{url: server.URL, client: server.Client()}
			value, err := reader.Read(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if test.want == nil && string(value) != `{"a":1,"b":2}` {
				t.Fatalf("canonical = %q", value)
			}
		})
	}
	contextValue, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &adminReader{url: "http://127.0.0.1:1", client: &http.Client{Timeout: time.Second}}
	if _, err := reader.Read(contextValue); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel got %v", err)
	}
}

func TestExecRunnerAndOutputBounds(t *testing.T) {
	var stdout bytes.Buffer
	request := commandRequest{kind: commandAdapt, configPath: "/tmp/config", workingDirectory: "/tmp", environment: []string{"PATH=/usr/bin:/bin"}}
	if err := (execRunner{}).Run(context.Background(), "/bin/true", request, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	contextValue, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (execRunner{}).Run(contextValue, "/bin/true", request, io.Discard, io.Discard); err == nil {
		t.Fatal("cancelled command succeeded")
	}
	if _, err := fixedArguments(commandRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid command got %v", err)
	}
	if _, err := fixedArguments(commandRequest{kind: commandReload, configPath: "/tmp/config", workingDirectory: "/tmp"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("reload without address got %v", err)
	}
	buffer := &limitedBuffer{limit: 2}
	if count, err := buffer.Write([]byte("abc")); count != 2 || !errors.Is(err, ErrLimit) {
		t.Fatalf("write = %d, %v", count, err)
	}
	if count, err := buffer.Write([]byte("x")); count != 0 || !errors.Is(err, ErrLimit) {
		t.Fatalf("full write = %d, %v", count, err)
	}
}

func TestBinaryReplacementIsDetectedBeforeCommand(t *testing.T) {
	fixture := newFixture(t)
	if err := os.WriteFile(fixture.options.CaddyBinary, []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, _, err := fixture.adapter.Preflight(context.Background(), Request{
		OperationID: "binary-change", ComponentID: "caddy-main",
		Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig)),
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestCommentsDoNotBecomeConfigurationInputs(t *testing.T) {
	valid := []string{
		"# import secret\n:80 { respond ok }\n",
		"# {$TOKEN}\n:80 { respond ok }\n",
		":80 { respond \"value # import secret\" }\n",
		":80 { respond `value # import secret` }\n",
	}
	for _, value := range valid {
		if err := validateConfiguration([]byte(value)); err != nil {
			t.Errorf("valid %q: %v", value, err)
		}
	}
	if !strings.Contains(stripComments(":80 { respond \"#\" } # removed\n"), "\"#\"") {
		t.Fatal("quoted comment marker was removed")
	}
	invalid := []string{
		":80 { import fragment.caddy respond ok }\n",
		":80 {\nrespond `first line\n{$TOKEN} # literal comment marker\n`\n}\n",
	}
	for _, value := range invalid {
		if err := validateConfiguration([]byte(value)); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid %q: %v", value, err)
		}
	}
}
