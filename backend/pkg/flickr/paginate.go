package flickr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultExtras is requested by list calls when ListOptions.Extras is empty.
const DefaultExtras = "url_sq,url_t,url_s,url_m,url_z,url_l,url_o,date_upload,last_update,owner_name,media,tags,description,license,o_dims,views"

// MaxPerPage is the largest page size Flickr serves for photo lists.
const MaxPerPage = 500

// ListOptions are the optional parameters shared by list calls. Zero values
// are omitted so Flickr applies its own defaults.
type ListOptions struct {
	Extras        string
	PerPage       int
	Page          int
	MinUploadDate time.Time
	MinFaveDate   time.Time
}

// params returns the request parameters for the options. withExtras controls
// whether the extras parameter (defaulting to DefaultExtras) is included.
func (o ListOptions) params(withExtras bool) map[string]string {
	params := make(map[string]string, 5)
	if withExtras {
		params["extras"] = DefaultExtras
		if extras := strings.TrimSpace(o.Extras); extras != "" {
			params["extras"] = extras
		}
	}
	if o.PerPage > 0 {
		params["per_page"] = strconv.Itoa(min(o.PerPage, MaxPerPage))
	}
	if o.Page > 0 {
		params["page"] = strconv.Itoa(o.Page)
	}
	if !o.MinUploadDate.IsZero() {
		params["min_upload_date"] = strconv.FormatInt(o.MinUploadDate.Unix(), 10)
	}
	if !o.MinFaveDate.IsZero() {
		params["min_fave_date"] = strconv.FormatInt(o.MinFaveDate.Unix(), 10)
	}
	return params
}

// Page is one page of a Flickr list.
type Page[T any] struct {
	Items   []T
	Page    int
	Pages   int
	Total   int
	PerPage int
}

// Paginator walks a Flickr list one page at a time. It fetches a page only
// when Next is called and never reads ahead.
type Paginator[T any] struct {
	fetch func(ctx context.Context, page int) (Page[T], error)
	next  int
	done  bool
	cur   Page[T]
	err   error
}

// NewPaginator creates a paginator that starts at page start (1 when lower)
// and loads pages with fetch.
func NewPaginator[T any](start int, fetch func(ctx context.Context, page int) (Page[T], error)) *Paginator[T] {
	return &Paginator[T]{fetch: fetch, next: max(start, 1)}
}

// Next fetches the next page. It returns false when the list is exhausted or
// a fetch failed; check Err afterwards.
func (p *Paginator[T]) Next(ctx context.Context) bool {
	if p.done || p.err != nil {
		return false
	}
	page, err := p.fetch(ctx, p.next)
	if err != nil {
		p.err = err
		return false
	}
	p.cur = page
	if page.Page > 0 {
		p.next = page.Page
	}
	p.next++
	p.done = p.next > page.Pages || len(page.Items) == 0
	return true
}

// Page returns the page loaded by the last successful Next.
func (p *Paginator[T]) Page() Page[T] { return p.cur }

// Err returns the error that stopped iteration, if any.
func (p *Paginator[T]) Err() error { return p.err }

// Int decodes a JSON number or a numeric string. Empty strings and null
// decode to zero.
type Int int64

func (i *Int) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "null" {
		return nil
	}
	text = strings.Trim(text, `"`)
	text = strings.TrimSpace(text)
	if text == "" {
		*i = 0
		return nil
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		*i = Int(n)
		return nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("flickr: invalid number %q", text)
	}
	*i = Int(f)
	return nil
}

// Int returns the value as an int.
func (i Int) Int() int { return int(i) }

// String decodes a JSON string or number into its text form (for identifiers
// that Flickr sometimes emits as numbers).
type String string

func (s *String) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if string(trimmed) == "null" {
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		*s = String(text)
		return nil
	}
	*s = String(trimmed)
	return nil
}

// Content decodes Flickr's {"_content": "..."} wrapper or a bare string.
type Content string

func (c *Content) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if string(trimmed) == "null" {
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var wrapped struct {
			Content String `json:"_content"`
		}
		if err := json.Unmarshal(trimmed, &wrapped); err != nil {
			return err
		}
		*c = Content(wrapped.Content)
		return nil
	}
	var value String
	if err := value.UnmarshalJSON(trimmed); err != nil {
		return err
	}
	*c = Content(value)
	return nil
}

// decodeRoot decodes the object stored under key in a Flickr response.
func decodeRoot(raw json.RawMessage, key string, out any) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return &CallError{Kind: ErrMalformedResponse, Cause: err}
	}
	body, ok := envelope[key]
	if !ok {
		return &CallError{Kind: ErrMalformedResponse, Message: fmt.Sprintf("response has no %q object", key)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &CallError{Kind: ErrMalformedResponse, Cause: err}
	}
	return nil
}

// decodePage decodes a paginated list stored under root with items under
// itemsKey. Flickr reports the page size as perpage or per_page.
func decodePage[T any](raw json.RawMessage, root, itemsKey string) (Page[T], error) {
	var body json.RawMessage
	if err := decodeRoot(raw, root, &body); err != nil {
		return Page[T]{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return Page[T]{}, &CallError{Kind: ErrMalformedResponse, Cause: err}
	}
	var meta struct {
		Page     Int `json:"page"`
		Pages    Int `json:"pages"`
		Total    Int `json:"total"`
		PerPage  Int `json:"perpage"`
		PerPage2 Int `json:"per_page"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return Page[T]{}, &CallError{Kind: ErrMalformedResponse, Cause: err}
	}
	page := Page[T]{Page: meta.Page.Int(), Pages: meta.Pages.Int(), Total: meta.Total.Int(), PerPage: meta.PerPage.Int()}
	if page.PerPage == 0 {
		page.PerPage = meta.PerPage2.Int()
	}
	if items, ok := fields[itemsKey]; ok {
		if err := json.Unmarshal(items, &page.Items); err != nil {
			return Page[T]{}, &CallError{Kind: ErrMalformedResponse, Cause: err}
		}
	}
	return page, nil
}

// call runs a method and returns the raw payload.
func (c *Client) call(ctx context.Context, creds Credentials, method string, params map[string]string) (json.RawMessage, error) {
	raw, _, err := c.Call(ctx, creds, method, params)
	return raw, err
}

// listPage runs a paginated method and decodes its page.
func listPage[T any](ctx context.Context, c *Client, creds Credentials, method, root, itemsKey string, params map[string]string) (Page[T], error) {
	raw, err := c.call(ctx, creds, method, params)
	if err != nil {
		return Page[T]{}, err
	}
	return decodePage[T](raw, root, itemsKey)
}

// getObject runs a method and decodes the object under root.
func getObject[T any](ctx context.Context, c *Client, creds Credentials, method, root string, params map[string]string) (T, error) {
	var out T
	raw, err := c.call(ctx, creds, method, params)
	if err != nil {
		return out, err
	}
	err = decodeRoot(raw, root, &out)
	return out, err
}

// decodeObject decodes the whole response object.
func decodeObject(raw []byte, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return &CallError{Kind: ErrMalformedResponse, Cause: err}
	}
	return nil
}
