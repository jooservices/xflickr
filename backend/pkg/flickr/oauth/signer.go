// Package oauth signs Flickr requests with OAuth 1.0a (RFC 5849 HMAC-SHA1)
// and runs the Flickr request-token, authorization, and access-token steps.
package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // OAuth 1.0a HMAC-SHA1 signing is mandated by the Flickr API
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const signatureMethod = "HMAC-SHA1"

// Signer produces OAuth 1.0a HMAC-SHA1 signatures. Now and NewNonce override
// the clock and nonce source so tests can replay a recorded exchange.
type Signer struct {
	ConsumerKey    string
	ConsumerSecret string
	Token          string
	TokenSecret    string
	Now            func() time.Time
	NewNonce       func() (string, error)
}

// Sign copies params, adds the OAuth protocol parameters, and sets
// oauth_signature. Query parameters already present on rawURL are covered
// by the signature and are not copied into the result.
func (s Signer) Sign(method, rawURL string, params url.Values) (url.Values, error) {
	if strings.TrimSpace(s.ConsumerKey) == "" {
		return nil, errors.New("oauth: consumer key is required")
	}
	nonceFn := s.NewNonce
	if nonceFn == nil {
		nonceFn = defaultNonce
	}
	nonce, err := nonceFn()
	if err != nil {
		return nil, fmt.Errorf("oauth: generate nonce: %w", err)
	}
	if nonce == "" {
		return nil, errors.New("oauth: nonce is empty")
	}

	signed := cloneValues(params)
	signed.Set("oauth_consumer_key", s.ConsumerKey)
	signed.Set("oauth_nonce", nonce)
	signed.Set("oauth_signature_method", signatureMethod)
	signed.Set("oauth_timestamp", s.timestamp())
	signed.Set("oauth_version", "1.0")
	if strings.TrimSpace(s.Token) != "" {
		signed.Set("oauth_token", s.Token)
	} else {
		signed.Del("oauth_token")
	}
	signed.Del("oauth_signature")

	signature, err := Signature(method, rawURL, signed, s.ConsumerSecret, s.TokenSecret)
	if err != nil {
		return nil, err
	}
	signed.Set("oauth_signature", signature)
	return signed, nil
}

func (s Signer) timestamp() string {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	return strconv.FormatInt(now().Unix(), 10)
}

// BaseString builds the RFC 5849 signature base string. params are the
// protocol and entity-body parameters; rawURL contributes its query.
// oauth_signature and realm are excluded.
func BaseString(method, rawURL string, params url.Values) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "", errors.New("oauth: method is required")
	}
	baseURI, err := baseStringURI(rawURL)
	if err != nil {
		return "", err
	}
	collected, err := collectParams(rawURL, params)
	if err != nil {
		return "", err
	}
	return method + "&" + PercentEncode(baseURI) + "&" + PercentEncode(normalize(collected)), nil
}

// Signature returns the base64 HMAC-SHA1 signature for method and rawURL.
func Signature(method, rawURL string, params url.Values, consumerSecret, tokenSecret string) (string, error) {
	base, err := BaseString(method, rawURL, params)
	if err != nil {
		return "", err
	}
	// RFC 5849 section 3.4.2 and Flickr require HMAC-SHA1. Each secret is
	// percent-encoded on its own, then joined with "&". An empty token secret
	// still contributes the trailing "&".
	key := PercentEncode(consumerSecret) + "&" + PercentEncode(tokenSecret)
	mac := hmac.New(sha1.New, []byte(key))
	_, _ = mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// PercentEncode encodes value per RFC 5849 section 3.6. Unreserved
// characters, including "~", are left as-is. Every other octet, including
// space, is percent-encoded with an uppercase hex value.
func PercentEncode(value string) string {
	var encoded strings.Builder
	encoded.Grow(len(value))
	for i := 0; i < len(value); i++ {
		octet := value[i]
		if isUnreserved(octet) {
			encoded.WriteByte(octet)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexDigits[octet>>4])
		encoded.WriteByte(hexDigits[octet&0x0F])
	}
	return encoded.String()
}

func baseStringURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("oauth: invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if parsed.Host == "" || (scheme != "http" && scheme != "https") {
		return "", fmt.Errorf("oauth: invalid URL %q", raw)
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := parsed.Port(); port != "" {
		if (scheme != "http" || port != "80") && (scheme != "https" || port != "443") {
			host += ":" + port
		}
	}
	if parsed.User != nil {
		host = parsed.User.String() + "@" + host
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return scheme + "://" + host + path, nil
}

func collectParams(rawURL string, extra url.Values) (url.Values, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("oauth: invalid URL: %w", err)
	}
	collected := make(url.Values)
	if parsed.RawQuery != "" {
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return nil, fmt.Errorf("oauth: invalid query: %w", err)
		}
		appendValues(collected, query)
	}
	appendValues(collected, extra)
	collected.Del("oauth_signature")
	collected.Del("realm")
	return collected, nil
}

func normalize(params url.Values) string {
	type pair struct {
		key   string
		value string
	}
	pairs := make([]pair, 0)
	for key, values := range params {
		encodedKey := PercentEncode(key)
		for _, value := range values {
			pairs = append(pairs, pair{key: encodedKey, value: PercentEncode(value)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key == pairs[j].key {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].key < pairs[j].key
	})
	var normalized strings.Builder
	for i, item := range pairs {
		if i > 0 {
			normalized.WriteByte('&')
		}
		normalized.WriteString(item.key)
		normalized.WriteByte('=')
		normalized.WriteString(item.value)
	}
	return normalized.String()
}

func appendValues(dst, src url.Values) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	appendValues(cloned, values)
	return cloned
}

func defaultNonce() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func isUnreserved(octet byte) bool {
	return (octet >= 'A' && octet <= 'Z') ||
		(octet >= 'a' && octet <= 'z') ||
		(octet >= '0' && octet <= '9') ||
		octet == '-' || octet == '.' || octet == '_' || octet == '~'
}

const hexDigits = "0123456789ABCDEF"

// EncodeQuery percent-encodes parameters for an OAuth query string.
// Space is "%20" and "~" is left unencoded, which is what Flickr records.
func EncodeQuery(params url.Values) string {
	type pair struct {
		key   string
		value string
	}
	pairs := make([]pair, 0)
	for key, values := range params {
		for _, value := range values {
			pairs = append(pairs, pair{key: key, value: value})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key == pairs[j].key {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].key < pairs[j].key
	})
	var encoded strings.Builder
	for i, item := range pairs {
		if i > 0 {
			encoded.WriteByte('&')
		}
		encoded.WriteString(PercentEncode(item.key))
		encoded.WriteByte('=')
		encoded.WriteString(PercentEncode(item.value))
	}
	return encoded.String()
}
