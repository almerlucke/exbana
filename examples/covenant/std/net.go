package std

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

const netSource = `
struct Response {
    status: Int,
    body: Text,
    headers: Map<Text, Text>
}

enum NetError {
    Denied
    Failed(message: Text)
}

trait Functions {
    fn get(url: Text) with net::fetch -> Result<Response, NetError>
    fn post(url: Text, body: Text, headers: Map<Text, Text>) with net::fetch -> Result<Response, NetError>
}
`

const envSource = `
enum EnvError {
    NotSet
    Denied
}

trait Functions {
    fn get(name: Text) with env::read -> Result<Text, EnvError>
}
`

const processSource = `
struct Output {
    code: Int,
    stdout: Text,
    stderr: Text
}

enum ProcessError {
    Denied
    Failed(message: Text)
}

trait Functions {
    fn run(program: Text, args: List<Text>) with process::run -> Result<Output, ProcessError>
}
`

// hostAllowed says whether a host is on the list: "*" is any, "example.com" is that host on any port, "example.com:8080" that
// port only, and "*.example.com" is every host under it.
func hostAllowed(u *url.URL, list []string) bool {
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	for _, pattern := range list {
		switch {
		case pattern == "*":
			return true
		case strings.HasPrefix(pattern, "*."):
			if strings.HasSuffix(u.Hostname(), pattern[1:]) {
				return true
			}
		case strings.Contains(pattern, ":"):
			if strings.EqualFold(u.Host, pattern) {
				return true
			}
		case strings.EqualFold(u.Hostname(), pattern):
			return true
		}
	}

	return false
}

var errDenied = errors.New("the host is not allowed")

// httpClient is the client that keeps to the policy: the hosts it goes to, the redirects it follows, the addresses it connects
// to, how long it waits.
func (s *Std) httpClient() *http.Client {
	if s.policy.Client != nil {
		return s.policy.Client
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		return s.client
	}

	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}

			ip := net.ParseIP(host)
			if !s.policy.AllowPrivateNet && ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
				return fmt.Errorf("%s is a private address", host)
			}

			return nil
		},
	}

	s.client = &http.Client{
		Timeout:   s.policy.timeout(),
		Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}

			if !hostAllowed(req.URL, s.policy.NetHosts) {
				return errDenied
			}

			return nil
		},
	}

	return s.client
}

func (s *Std) request(ctx context.Context, method, rawURL, body string, headers *run.Map) (run.Value, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !hostAllowed(u, s.policy.NetHosts) {
		return s.denied("net", "NetError"), nil
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
	if err != nil {
		return s.fail("net", "NetError", "Failed", err.Error()), nil
	}

	if headers != nil {
		for i := range headers.Len() {
			k, v := headers.Entry(i)
			req.Header.Set(k.(string), v.(string))
		}
	}

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if ctx.Err() != nil {
				return nil, &run.Limit{Kind: "cancelled", Err: ctx.Err()}
			}
		}

		if errors.Is(err, errDenied) {
			return s.denied("net", "NetError"), nil
		}

		return s.fail("net", "NetError", "Failed", cleanNetError(err)), nil
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, s.policy.maxData()+1))
	if err != nil {
		return s.fail("net", "NetError", "Failed", err.Error()), nil
	}

	if int64(len(data)) > s.policy.maxData() {
		return s.fail("net", "NetError", "Failed", "the response is larger than the most that is read"), nil
	}

	names := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		names = append(names, k)
	}

	sort.Strings(names)

	hm := run.NewMap()
	for _, k := range names {
		hm = hm.Set(strings.ToLower(k), strings.Join(resp.Header.Values(k), ", "))
	}

	out, err := s.prog.Struct("net", "Response", nil, int64(resp.StatusCode), string(data), hm)
	if err != nil {
		return nil, err
	}

	return s.prog.Ok(out), nil
}

// cleanNetError is the message of an error of the client without the url that is in it: the program knows the url.
func cleanNetError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}

	return err.Error()
}

func (s *Std) netHost(h map[string]run.HostFunc) {
	h["net::get"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		return s.request(ctx, http.MethodGet, a[0].(string), "", nil)
	}

	h["net::post"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		return s.request(ctx, http.MethodPost, a[0].(string), a[1].(string), a[2].(*run.Map))
	}
}
