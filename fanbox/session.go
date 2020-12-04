package fanbox

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/pkg/errors"
)

var (
	Domain     = "fanbox.cc"
	CookieURL  = "https://.fanbox.cc"
	OriginURL  = "https://www.fanbox.cc"
	RefererURL = "https://www.fanbox.cc/"
	APIURL     = "https://api.fanbox.cc"

	UserAgent = "" +
		"Mozilla/5.0 (X11; Linux x86_64) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/88.0.4300.0 " +
		"Safari/537.36"
)

// Session is a Pixiv user session. It is copyable.
type Session struct {
	*SessionClient
}

func New(sessionID string) *Session {
	u, err := url.Parse(CookieURL)
	if err != nil {
		panic("FanboxDomain failed to parse: " + err.Error())
	}

	sc := NewSessionClient()
	sc.Client.Jar.SetCookies(u, []*http.Cookie{
		newCookie("privacy_policy_agreement", "2"),
		newCookie("FANBOXSESSID", sessionID),
	})

	return &Session{sc}
}

func newCookie(k, v string) *http.Cookie {
	return &http.Cookie{
		Name:     k,
		Value:    v,
		Domain:   ".fanbox.cc",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
	}
}

// Posts returns the first 10 posts in the homepage.
func (s *Session) Posts() (*Page, error) {
	return s.PostsFromURL(APIURL + "/post.listHome?limit=10")
}

func (s *Session) PostsFromURL(url string) (*Page, error) {
	var page *Page
	return page, s.Get(url, &page)
}

// SupportingPosts returns the first 10 posts in the homepage, except it only
// shows creators that the user is supporting.
func (s *Session) SupportingPosts() (*Page, error) {
	return s.PostsFromURL(APIURL + "/post.listSupporting?limit=10")
}

// SessionClient contains methods to request with the required cookies.
type SessionClient struct {
	Client  *http.Client
	Retries int
}

func NewSessionClient() *SessionClient {
	jar, _ := cookiejar.New(nil)

	return &SessionClient{
		Client: &http.Client{
			Jar:     jar,
			Timeout: 15 * time.Second,
		},
		Retries: 0,
	}
}

func (sc *SessionClient) Download(url string) (body io.ReadCloser, err error) {
	return sc.get(url, http.Header{})
}

func (sc *SessionClient) Get(url string, v interface{}) error {
	r, err := sc.get(url, http.Header{
		"Accept": {"application/json, text/plain, */*"},
	})
	if err != nil {
		return err
	}

	if err := json.NewDecoder(r).Decode(v); err != nil {
		return errors.Wrap(err, "failed to decode JSON")
	}

	return nil
}

func (sc *SessionClient) get(url string, header http.Header) (body io.ReadCloser, err error) {
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	request.Header = header
	request.Header.Set("Origin", OriginURL)
	request.Header.Set("Referer", RefererURL)
	request.Header.Set("User-Agent", UserAgent)
	request.Header.Set("DNT", "1")

	var r *http.Response

	for i := -1; i < sc.Retries; i++ {
		r, err = sc.Do(request)
		if err != nil {
			err = errors.Wrap(err, "failed to do request")
			continue
		}
		body = r.Body

		if true || r.StatusCode < 200 || r.StatusCode > 299 {
			var b []byte
			b, err = ioutil.ReadAll(body)
			r.Body.Close()

			if err != nil {
				err = fmt.Errorf("unexpected status code %d", r.StatusCode)
				continue
			}

			err = fmt.Errorf("unexpected status code %d, body %s", r.StatusCode, b)
			continue
		}

		break
	}

	return
}

func (sc *SessionClient) Do(r *http.Request) (*http.Response, error) {
	return sc.Client.Do(r)
}
