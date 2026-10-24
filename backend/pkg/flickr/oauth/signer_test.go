package oauth

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPercentEncode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "abcXYZ09-._~", want: "abcXYZ09-._~"},
		{in: " ", want: "%20"},
		{in: "*", want: "%2A"},
		{in: "~", want: "~"},
		{in: "café", want: "caf%C3%A9"},
		{in: "snow man ☃ ~ *", want: "snow%20man%20%E2%98%83%20~%20%2A"},
		{in: "a+b=c@", want: "a%2Bb%3Dc%40"},
	}
	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			t.Parallel()
			if got := PercentEncode(test.in); got != test.want {
				t.Fatalf("PercentEncode(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestBaseStringEncodingEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		rawURL string
		params url.Values
		want   string
	}{
		{
			name:   "tilde stays unencoded",
			rawURL: "http://example.com/?q=~",
			want:   "GET&http%3A%2F%2Fexample.com%2F&q%3D~",
		},
		{
			name:   "space",
			rawURL: "http://example.com/",
			params: url.Values{"q": {"a b"}},
			want:   "GET&http%3A%2F%2Fexample.com%2F&q%3Da%2520b",
		},
		{
			name:   "unicode",
			rawURL: "http://example.com/",
			params: url.Values{"q": {"é"}},
			want:   "GET&http%3A%2F%2Fexample.com%2F&q%3D%25C3%25A9",
		},
		{
			name:   "plus in query is a space",
			rawURL: "http://example.com/?a2=r+b",
			want:   "GET&http%3A%2F%2Fexample.com%2F&a2%3Dr%2520b",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := BaseString("GET", test.rawURL, test.params)
			if err != nil {
				t.Fatalf("BaseString() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("BaseString() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestBaseStringURINormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rawURL string
		want   string
	}{
		{rawURL: "HTTP://Example.COM:80", want: "GET&http%3A%2F%2Fexample.com%2F&"},
		{rawURL: "https://Example.COM:443/Path", want: "GET&https%3A%2F%2Fexample.com%2FPath&"},
		{rawURL: "http://example.com:8080", want: "GET&http%3A%2F%2Fexample.com%3A8080%2F&"},
		{rawURL: "http://example.com/request#frag", want: "GET&http%3A%2F%2Fexample.com%2Frequest&"},
		{rawURL: "http://user:secret@example.com/x", want: "GET&http%3A%2F%2Fuser%3Asecret%40example.com%2Fx&"},
		{rawURL: "http://[::1]:8080/x", want: "GET&http%3A%2F%2F%5B%3A%3A1%5D%3A8080%2Fx&"},
	}
	for _, test := range tests {
		t.Run(test.rawURL, func(t *testing.T) {
			t.Parallel()
			got, err := BaseString("get", test.rawURL, nil)
			if err != nil {
				t.Fatalf("BaseString() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("BaseString() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestBaseStringRejectsBadInput(t *testing.T) {
	t.Parallel()
	if _, err := BaseString("  ", "http://example.com/", nil); err == nil || !strings.Contains(err.Error(), "method is required") {
		t.Fatalf("empty method error = %v", err)
	}
	for _, rawURL := range []string{"ftp://example.com/", "example.com/path", "http://", "://bad"} {
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()
			if _, err := BaseString("GET", rawURL, nil); err == nil {
				t.Fatal("BaseString() error = nil")
			}
		})
	}
	if _, err := BaseString("GET", "http://example.com/?q=%", nil); err == nil || !strings.Contains(err.Error(), "invalid query") {
		t.Fatalf("bad query error = %v", err)
	}
}

func TestRFC5849Vectors(t *testing.T) {
	t.Parallel()

	// Section 3.4.1 parameter normalisation. Errata 2550 replaces the signature
	// printed in the RFC; that value was calculated as if the method were GET.
	postParams := url.Values{}
	postParams.Add("c2", "")
	postParams.Add("a3", "2 q")
	postParams.Set("oauth_consumer_key", "9djdj82h48djs9d2")
	postParams.Set("oauth_token", "kkk9d7dh3k39sjv7")
	postParams.Set("oauth_signature_method", "HMAC-SHA1")
	postParams.Set("oauth_timestamp", "137131201")
	postParams.Set("oauth_nonce", "7d8f3e4a")
	postParams.Set("realm", "Example")
	postParams.Set("oauth_signature", "ignored")
	assertSignature(t, "POST", "http://example.com/request?b5=%3D%253D&a3=a&c%40=&a2=r%20b", postParams,
		"j49sk3j29djd", "dh893hdasih9",
		"POST&http%3A%2F%2Fexample.com%2Frequest&a2%3Dr%2520b%26a3%3D2%2520q%26a3%3Da%26b5%3D%253D%25253D%26c%2540%3D%26c2%3D%26oauth_consumer_key%3D9djdj82h48djs9d2%26oauth_nonce%3D7d8f3e4a%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D137131201%26oauth_token%3Dkkk9d7dh3k39sjv7",
		"r6/TJjbCOr97/+UU0NsvSne7s5g=")

	// Section 1.2 temporary credentials. The advertised endpoint is HTTPS.
	initiate := url.Values{}
	initiate.Set("oauth_consumer_key", "dpf43f3p2l4k3l03")
	initiate.Set("oauth_signature_method", "HMAC-SHA1")
	initiate.Set("oauth_timestamp", "137131200")
	initiate.Set("oauth_nonce", "wIjqoS")
	initiate.Set("oauth_callback", "http://printer.example.com/ready")
	assertSignature(t, "POST", "https://photos.example.net/initiate", initiate,
		"kd94hf93k423kf44", "",
		"POST&https%3A%2F%2Fphotos.example.net%2Finitiate&oauth_callback%3Dhttp%253A%252F%252Fprinter.example.com%252Fready%26oauth_consumer_key%3Ddpf43f3p2l4k3l03%26oauth_nonce%3DwIjqoS%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D137131200",
		"74KNZJeDHnMBp0EMJ9ZHt/XKycU=")

	// Section 1.2 token credentials.
	token := url.Values{}
	token.Set("oauth_consumer_key", "dpf43f3p2l4k3l03")
	token.Set("oauth_token", "hh5s93j4hdidpola")
	token.Set("oauth_signature_method", "HMAC-SHA1")
	token.Set("oauth_timestamp", "137131201")
	token.Set("oauth_nonce", "walatlh")
	token.Set("oauth_verifier", "hfdp7dh39dks9884")
	assertSignature(t, "POST", "https://photos.example.net/token", token,
		"kd94hf93k423kf44", "hdhd0244k9j7ao03",
		"POST&https%3A%2F%2Fphotos.example.net%2Ftoken&oauth_consumer_key%3Ddpf43f3p2l4k3l03%26oauth_nonce%3Dwalatlh%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D137131201%26oauth_token%3Dhh5s93j4hdidpola%26oauth_verifier%3Dhfdp7dh39dks9884",
		"gKgrFCywp7rO0OXSjdot/IHF7IU=")

	// Section 1.2 protected resource. The example request is HTTP.
	assertSignature(t, "GET", "http://photos.example.net/photos?file=vacation.jpg&size=original", url.Values{
		"oauth_consumer_key":     {"dpf43f3p2l4k3l03"},
		"oauth_token":            {"nnch734d00sl2jdk"},
		"oauth_signature_method": {"HMAC-SHA1"},
		"oauth_timestamp":        {"137131202"},
		"oauth_nonce":            {"chapoH"},
	}, "kd94hf93k423kf44", "pfkkdhi9sl3r4s00",
		"GET&http%3A%2F%2Fphotos.example.net%2Fphotos&file%3Dvacation.jpg%26oauth_consumer_key%3Ddpf43f3p2l4k3l03%26oauth_nonce%3DchapoH%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D137131202%26oauth_token%3Dnnch734d00sl2jdk%26size%3Doriginal",
		"MdpQcU8iPSUjWoN/UDMsK2sui9I=")
}

func TestRecordedFlickrExchange(t *testing.T) {
	t.Parallel()

	// Recorded exchange published at https://www.flickr.com/services/api/auth.oauth.html.
	// The nonce and timestamp that match the published base string are the ones
	// in the request-token URL, not the earlier mismatched pair. Flickr did not
	// publish the consumer secret, so the recorded check is the base string
	// (the signature input). The printed signature cannot be reproduced.
	requestToken := url.Values{}
	requestToken.Set("oauth_nonce", "95613465")
	requestToken.Set("oauth_timestamp", "1305586162")
	requestToken.Set("oauth_consumer_key", "653e7a6ecc1d528c516cc8f92cf98611")
	requestToken.Set("oauth_signature_method", "HMAC-SHA1")
	requestToken.Set("oauth_version", "1.0")
	requestToken.Set("oauth_callback", "http://www.example.com")
	base, err := BaseString("GET", "https://www.flickr.com/services/oauth/request_token", requestToken)
	if err != nil {
		t.Fatalf("request token BaseString() error = %v", err)
	}
	const wantRequest = "GET&https%3A%2F%2Fwww.flickr.com%2Fservices%2Foauth%2Frequest_token&oauth_callback%3Dhttp%253A%252F%252Fwww.example.com%26oauth_consumer_key%3D653e7a6ecc1d528c516cc8f92cf98611%26oauth_nonce%3D95613465%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D1305586162%26oauth_version%3D1.0"
	if base != wantRequest {
		t.Fatalf("request token base = %s", base)
	}

	access := url.Values{}
	access.Set("oauth_nonce", "37026218")
	access.Set("oauth_timestamp", "1305586309")
	access.Set("oauth_verifier", "5d1b96a26b494074")
	access.Set("oauth_consumer_key", "653e7a6ecc1d528c516cc8f92cf98611")
	access.Set("oauth_signature_method", "HMAC-SHA1")
	access.Set("oauth_version", "1.0")
	access.Set("oauth_token", "72157626737672178-022bbd2f4c2f3432")
	base, err = BaseString("GET", "https://www.flickr.com/services/oauth/access_token", access)
	if err != nil {
		t.Fatalf("access token BaseString() error = %v", err)
	}
	const wantAccess = "GET&https%3A%2F%2Fwww.flickr.com%2Fservices%2Foauth%2Faccess_token&oauth_consumer_key%3D653e7a6ecc1d528c516cc8f92cf98611%26oauth_nonce%3D37026218%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D1305586309%26oauth_token%3D72157626737672178-022bbd2f4c2f3432%26oauth_verifier%3D5d1b96a26b494074%26oauth_version%3D1.0"
	if base != wantAccess {
		t.Fatalf("access token base = %s", base)
	}

	login := url.Values{}
	login.Set("nojsoncallback", "1")
	login.Set("format", "json")
	login.Set("oauth_nonce", "84354935")
	login.Set("oauth_consumer_key", "653e7a6ecc1d528c516cc8f92cf98611")
	login.Set("oauth_timestamp", "1305583871")
	login.Set("oauth_signature_method", "HMAC-SHA1")
	login.Set("oauth_version", "1.0")
	login.Set("oauth_token", "72157626318069415-087bfc7b5816092c")
	login.Set("method", "flickr.test.login")
	base, err = BaseString("GET", "https://www.flickr.com/services/rest", login)
	if err != nil {
		t.Fatalf("login BaseString() error = %v", err)
	}
	const wantLogin = "GET&https%3A%2F%2Fwww.flickr.com%2Fservices%2Frest&format%3Djson%26method%3Dflickr.test.login%26nojsoncallback%3D1%26oauth_consumer_key%3D653e7a6ecc1d528c516cc8f92cf98611%26oauth_nonce%3D84354935%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D1305583871%26oauth_token%3D72157626318069415-087bfc7b5816092c%26oauth_version%3D1.0"
	if base != wantLogin {
		t.Fatalf("login base = %s", base)
	}
}

func TestSignatureEncodesSecrets(t *testing.T) {
	t.Parallel()
	got, err := Signature("POST", "http://example.com/", url.Values{"a": {"1"}}, "a b", "c~d")
	if err != nil {
		t.Fatalf("Signature() error = %v", err)
	}
	// Key is percentEncode("a b") + "&" + percentEncode("c~d") = "a%20b&c~d".
	if got != "xUvagFB/JBltWiuMhHi+zo2cW9c=" {
		t.Fatalf("Signature() = %s", got)
	}
}

func TestSignerInjectsNonceAndTimestamp(t *testing.T) {
	t.Parallel()
	signer := Signer{
		ConsumerKey:    "653e7a6ecc1d528c516cc8f92cf98611",
		ConsumerSecret: "consumer secret",
		Now:            func() time.Time { return time.Unix(1305586162, 0).UTC() },
		NewNonce:       func() (string, error) { return "95613465", nil },
	}
	params := url.Values{"oauth_callback": {"http://www.example.com"}, "oauth_token": {"drop-me"}}
	signed, err := signer.Sign("GET", "https://www.flickr.com/services/oauth/request_token", params)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if signed.Get("oauth_nonce") != "95613465" || signed.Get("oauth_timestamp") != "1305586162" {
		t.Fatalf("protocol params = nonce %q timestamp %q", signed.Get("oauth_nonce"), signed.Get("oauth_timestamp"))
	}
	if signed.Get("oauth_version") != "1.0" || signed.Get("oauth_signature_method") != "HMAC-SHA1" {
		t.Fatalf("method/version = %q %q", signed.Get("oauth_signature_method"), signed.Get("oauth_version"))
	}
	if signed.Get("oauth_token") != "" {
		t.Fatalf("oauth_token = %q, want empty when the signer has no token", signed.Get("oauth_token"))
	}
	if params.Get("oauth_nonce") != "" {
		t.Fatal("Sign mutated the caller's parameters")
	}
	want, err := Signature("GET", "https://www.flickr.com/services/oauth/request_token", url.Values{
		"oauth_callback":         {"http://www.example.com"},
		"oauth_consumer_key":     {signer.ConsumerKey},
		"oauth_nonce":            {"95613465"},
		"oauth_signature_method": {"HMAC-SHA1"},
		"oauth_timestamp":        {"1305586162"},
		"oauth_version":          {"1.0"},
	}, "consumer secret", "")
	if err != nil {
		t.Fatalf("Signature() error = %v", err)
	}
	if signed.Get("oauth_signature") != want {
		t.Fatalf("signature = %s, want %s", signed.Get("oauth_signature"), want)
	}

	withToken := Signer{
		ConsumerKey:    signer.ConsumerKey,
		ConsumerSecret: "consumer secret",
		Token:          "token-1",
		TokenSecret:    "token~secret",
		Now:            func() time.Time { return time.Unix(1305583871, 0) },
		NewNonce:       func() (string, error) { return "84354935", nil },
	}
	signed, err = withToken.Sign("POST", "https://www.flickr.com/services/rest", url.Values{
		"method":         {"flickr.test.login"},
		"api_key":        {signer.ConsumerKey},
		"format":         {"json"},
		"nojsoncallback": {"1"},
		"text":           {"hello world"},
		"title":          {"café~x"},
	})
	if err != nil {
		t.Fatalf("Sign() with token error = %v", err)
	}
	if signed.Get("oauth_signature") != "YLg/VnHsOg+kN8s+hIWaEjCbVOU=" {
		t.Fatalf("signed call signature = %s", signed.Get("oauth_signature"))
	}
}

func TestSignerRejectsBadNonceAndKey(t *testing.T) {
	t.Parallel()
	signer := Signer{ConsumerSecret: "secret"}
	if _, err := signer.Sign("GET", "https://example.com/", nil); err == nil {
		t.Fatal("missing consumer key error = nil")
	}
	signer.ConsumerKey = "key"
	signer.NewNonce = func() (string, error) { return "", errors.New("entropy unavailable") }
	if _, err := signer.Sign("GET", "https://example.com/", nil); err == nil || !strings.Contains(err.Error(), "entropy unavailable") {
		t.Fatalf("nonce error = %v", err)
	}
	signer.NewNonce = func() (string, error) { return "", nil }
	if _, err := signer.Sign("GET", "https://example.com/", nil); err == nil || !strings.Contains(err.Error(), "nonce is empty") {
		t.Fatalf("empty nonce error = %v", err)
	}
	signer.NewNonce = func() (string, error) { return "n", nil }
	if _, err := signer.Sign("GET", "ftp://example.com/", nil); err == nil {
		t.Fatal("bad URL error = nil")
	}
}

func TestDefaultNonceAndClock(t *testing.T) {
	t.Parallel()
	signer := Signer{ConsumerKey: "key", ConsumerSecret: "secret"}
	first, err := signer.Sign("GET", "https://example.com/a", nil)
	if err != nil {
		t.Fatalf("first Sign() error = %v", err)
	}
	second, err := signer.Sign("GET", "https://example.com/a", nil)
	if err != nil {
		t.Fatalf("second Sign() error = %v", err)
	}
	if len(first.Get("oauth_nonce")) != 32 || first.Get("oauth_nonce") == second.Get("oauth_nonce") {
		t.Fatalf("nonces = %q %q", first.Get("oauth_nonce"), second.Get("oauth_nonce"))
	}
	stamp, err := parseUnix(first.Get("oauth_timestamp"))
	if err != nil {
		t.Fatalf("timestamp %q: %v", first.Get("oauth_timestamp"), err)
	}
	if delta := time.Since(time.Unix(stamp, 0)); delta < -2*time.Second || delta > 2*time.Minute {
		t.Fatalf("timestamp delta = %s", delta)
	}
}

func TestEncodeQuerySortsAndEncodes(t *testing.T) {
	t.Parallel()
	got := EncodeQuery(url.Values{"b": {"a b"}, "a": {"~", "é"}})
	want := "a=~&a=%C3%A9&b=a%20b"
	if got != want {
		t.Fatalf("EncodeQuery() = %s, want %s", got, want)
	}
}

func assertSignature(t *testing.T, method, rawURL string, params url.Values, consumerSecret, tokenSecret, wantBase, wantSignature string) {
	t.Helper()
	base, err := BaseString(method, rawURL, params)
	if err != nil {
		t.Fatalf("BaseString() error = %v", err)
	}
	if base != wantBase {
		t.Fatalf("BaseString() = %s\nwant %s", base, wantBase)
	}
	signature, err := Signature(method, rawURL, params, consumerSecret, tokenSecret)
	if err != nil {
		t.Fatalf("Signature() error = %v", err)
	}
	if signature != wantSignature {
		t.Fatalf("Signature() = %s, want %s", signature, wantSignature)
	}
}

func parseUnix(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}
