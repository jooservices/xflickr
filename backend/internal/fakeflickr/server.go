package fakeflickr

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Server is a local Flickr-compatible REST, OAuth, and image server.
type Server struct {
	dataset   Dataset
	scenarios *Scenarios
	mux       *http.ServeMux
	mu        sync.Mutex
	callbacks map[string]string
}

//go:embed testdata/seed.jpg
var seedJPEG []byte

func NewServer(dataset Dataset, scenarios *Scenarios) *Server {
	if scenarios == nil {
		scenarios = NewScenarios(ScenarioConfig{})
	}
	s := &Server{dataset: dataset, scenarios: scenarios, mux: http.NewServeMux(), callbacks: make(map[string]string)}
	s.mux.HandleFunc("/services/rest", s.rest)
	s.mux.HandleFunc("/services/oauth/request_token", s.requestToken)
	s.mux.HandleFunc("/services/oauth/access_token", s.accessToken)
	s.mux.HandleFunc("/services/oauth/authorize", s.authorize)
	s.mux.HandleFunc("/__admin/scenarios", s.adminScenarios)
	s.mux.HandleFunc("/images/", s.image)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) adminScenarios(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var config ScenarioConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&config); err != nil {
		http.Error(w, "invalid scenario config", http.StatusBadRequest)
		return
	}
	s.scenarios.Configure(config)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.scenarios.before(w, r) {
		return
	}
	if s.scenarios.revoked(r.Form.Get("oauth_token")) {
		flickrError(w, 98, "Invalid auth token")
		return
	}
	method := r.Form.Get("method")
	if !strings.HasPrefix(method, "flickr.") {
		flickrError(w, 1, "Unknown method")
		return
	}
	data, ok := s.response(strings.TrimPrefix(method, "flickr."), r)
	if !ok {
		flickrError(w, 1, "Unknown method")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func flickrError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"stat": "fail", "code": code, "message": message})
}
func content(value string) map[string]string { return map[string]string{"_content": value} }

//nolint:cyclop // Flickr method dispatch intentionally maps each typed method to its wire envelope.
func (s *Server) response(method string, r *http.Request) (map[string]any, bool) {
	person := s.person(r.Form.Get("user_id"))
	photo := s.photo(r.Form.Get("photo_id"))
	photos := s.photosFor(person.NSID)
	photoRows := make([]any, 0, len(photos))
	for _, p := range photos {
		photoRows = append(photoRows, s.photoJSON(p))
	}
	contacts := make([]any, 0)
	for _, c := range s.dataset.Contacts {
		if c.Owner == person.NSID {
			contacts = append(contacts, map[string]any{"nsid": c.NSID, "username": s.person(c.NSID).Username, "realname": s.person(c.NSID).RealName, "friend": boolInt(c.Friend), "family": boolInt(c.Family)})
		}
	}
	page := func(field, singular string, rows []any, total int) map[string]any {
		p, per := parsePositive(r.Form.Get("page"), 1), parsePositive(r.Form.Get("per_page"), 100)
		start := (p - 1) * per
		if start > len(rows) {
			start = len(rows)
		}
		end := min(start+per, len(rows))
		return map[string]any{"stat": "ok", field: map[string]any{"page": p, "pages": max(1, (total+per-1)/per), "perpage": per, "total": total, singular: rows[start:end]}}
	}
	photoPage := func(field string, rows []any) map[string]any { return page(field, "photo", rows, len(rows)) }
	switch method {
	case "test.echo":
		fields := map[string]any{"stat": "ok"}
		for k, v := range r.Form {
			if k != "api_key" && k != "format" && k != "nojsoncallback" && k != "method" {
				fields[k] = v[0]
			}
		}
		return fields, true
	case "test.login":
		return map[string]any{"stat": "ok", "user": map[string]any{"id": person.NSID, "username": content(person.Username)}}, true
	case "people.getInfo":
		return map[string]any{"stat": "ok", "person": s.personJSON(person)}, true
	case "people.getPhotos", "people.getPublicPhotos", "favorites.getList", "favorites.getPublicList", "photosets.getPhotos", "galleries.getPhotos", "groups.pools.getPhotos":
		if method == "favorites.getList" || method == "favorites.getPublicList" {
			photoRows = s.favoritePhotos(person.NSID)
		}
		if method == "photosets.getPhotos" {
			albumPhotos := []any{}
			for _, album := range s.dataset.Albums {
				if album.ID == r.Form.Get("photoset_id") {
					for _, id := range album.Photos {
						albumPhotos = append(albumPhotos, s.photoJSON(s.photo(id)))
					}
				}
			}
			p := parsePositive(r.Form.Get("page"), 1)
			per := parsePositive(r.Form.Get("per_page"), 100)
			return map[string]any{"stat": "ok", "photoset": map[string]any{"id": r.Form.Get("photoset_id"), "page": p, "pages": max(1, (len(albumPhotos)+per-1)/per), "perpage": per, "total": len(albumPhotos), "photo": albumPhotos}}, true
		}
		return photoPage("photos", photoRows), true
	case "contacts.getList", "contacts.getPublicList":
		return page("contacts", "contact", contacts, len(contacts)), true
	case "photos.getContactsPhotos":
		return photoPage("photos", photoRows), true
	case "photos.getSizes":
		return map[string]any{"stat": "ok", "sizes": map[string]any{"canblog": 0, "canprint": 0, "candownload": 1, "size": []any{s.sizeJSON(photo, "Square", 75, 75), s.sizeJSON(photo, "Small", 240, 160), s.sizeJSON(photo, "Large", 1200, 800), s.sizeJSON(photo, "Original", 2400, 1600)}}}, true
	case "photos.getInfo":
		return map[string]any{"stat": "ok", "photo": map[string]any{"id": photo.ID, "secret": "seedsecret", "server": "1", "farm": 1, "dateuploaded": "1700000000", "isfavorite": 0, "license": "0", "safety_level": 0, "rotation": 0, "originalsecret": "seedsecret", "originalformat": "jpg", "owner": map[string]any{"nsid": photo.Owner, "username": s.person(photo.Owner).Username, "path_alias": s.person(photo.Owner).Username}, "title": content(photo.Title), "description": content("Seeded public image"), "visibility": map[string]any{"ispublic": 1, "isfriend": 0, "isfamily": 0}, "dates": map[string]any{"posted": "1700000000", "taken": "2023-11-14 00:00:00", "takengranularity": 0, "takenunknown": 0}, "tags": map[string]any{"tag": []any{}}, "urls": map[string]any{"url": []any{map[string]any{"type": "photopage", "_content": "http://localhost:8088/photo/" + photo.ID}}}}}, true
	case "photos.getExif":
		return map[string]any{"stat": "ok", "photo": map[string]any{"id": photo.ID, "secret": "seedsecret", "server": "1", "farm": 1, "camera": "Fake Camera", "exif": []any{}}}, true
	case "photos.getAllContexts":
		return map[string]any{"stat": "ok", "set": []any{}, "pool": []any{}}, true
	case "photos.comments.getList":
		return page("comments", "comment", []any{}, 0), true
	case "photos.getFavorites":
		rows := []any{}
		for _, f := range s.dataset.Favorites {
			if f.Photo == photo.ID {
				p := s.person(f.Person)
				rows = append(rows, map[string]any{"nsid": p.NSID, "username": p.Username, "realname": p.RealName, "favedate": f.Date})
			}
		}
		return page("photo", "person", rows, len(rows)), true
	case "photos.people.getList":
		return page("people", "person", []any{}, 0), true
	case "photosets.getList":
		rows := []any{}
		for _, a := range s.dataset.Albums {
			if a.Owner == person.NSID {
				rows = append(rows, s.albumJSON(a))
			}
		}
		return page("photosets", "photoset", rows, len(rows)), true
	case "galleries.getList":
		rows := []any{}
		for _, g := range s.dataset.Galleries {
			if g.Owner == person.NSID {
				rows = append(rows, map[string]any{"id": g.ID, "gallery_id": g.ID, "owner": g.Owner, "username": person.Username, "primary_photo_id": first(g.Photos), "count_photos": len(g.Photos), "count_total": len(g.Photos), "count_comments": 0, "title": content(g.Title), "description": content("Seed gallery")})
			}
		}
		return page("galleries", "gallery", rows, len(rows)), true
	case "people.getGroups", "people.getPublicGroups":
		rows := []any{}
		for _, g := range s.dataset.Groups {
			rows = append(rows, map[string]any{"nsid": g.ID, "name": g.Name, "admin": 0, "members": len(g.Members), "pool_count": len(g.Photos)})
		}
		return page("groups", "group", rows, len(rows)), true
	case "groups.getInfo":
		g := Group{}
		if len(s.dataset.Groups) > 0 {
			g = s.dataset.Groups[0]
		}
		return map[string]any{"stat": "ok", "group": map[string]any{"id": g.ID, "name": content(g.Name), "description": content("Seed group"), "members": content(strconv.Itoa(len(g.Members))), "pool_count": content(strconv.Itoa(len(g.Photos))), "privacy": content("3"), "rules": content("")}}, true
	default:
		return nil, false
	}
}

func (s *Server) person(id string) Person {
	for _, p := range s.dataset.People {
		if p.NSID == id {
			return p
		}
	}
	if len(s.dataset.People) > 0 {
		return s.dataset.People[0]
	}
	return Person{}
}

func (s *Server) photo(id string) Photo {
	for _, p := range s.dataset.Photos {
		if p.ID == id {
			return p
		}
	}
	if len(s.dataset.Photos) > 0 {
		return s.dataset.Photos[0]
	}
	return Photo{}
}

func (s *Server) photosFor(owner string) []Photo {
	out := []Photo{}
	for _, p := range s.dataset.Photos {
		if p.Owner == owner {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = append(out, s.dataset.Photos...)
	}
	return out
}

func (s *Server) favoritePhotos(owner string) []any {
	rows := []any{}
	for _, favorite := range s.dataset.Favorites {
		if favorite.Person == owner {
			rows = append(rows, s.photoJSON(s.photo(favorite.Photo)))
		}
	}
	return rows
}

func (s *Server) personJSON(p Person) map[string]any {
	return map[string]any{"id": p.NSID, "nsid": p.NSID, "ispro": 0, "is_deleted": 0, "iconserver": "0", "iconfarm": 0, "path_alias": p.Username, "has_stats": 1, "username": content(p.Username), "realname": content(p.RealName), "location": content(""), "description": content("Seed user"), "photosurl": content("http://localhost:8088/photos/" + p.Username), "profileurl": content("http://localhost:8088/people/" + p.NSID), "mobileurl": content(""), "photos": map[string]any{"firstdate": content("1700000000"), "firstdatetaken": content("2023-11-14 00:00:00"), "count": content(strconv.Itoa(len(s.photosFor(p.NSID))))}}
}

func (s *Server) photoJSON(p Photo) map[string]any {
	url := "http://localhost:8088/images/" + p.ID + ".jpg"
	return map[string]any{"id": p.ID, "owner": p.Owner, "secret": "seedsecret", "server": "1", "farm": 1, "title": p.Title, "ispublic": 1, "isfriend": 0, "isfamily": 0, "ownername": s.person(p.Owner).Username, "dateupload": 1700000000, "lastupdate": 1700000100, "date_faved": 1700000200, "views": 1, "tags": "seed", "license": "0", "media": "photo", "description": content("Seeded public image"), "o_width": 2400, "o_height": 1600, "url_sq": url, "url_t": url, "url_s": url, "url_m": url, "url_z": url, "url_l": url, "url_o": url, "width_o": 2400, "height_o": 1600}
}

func (s *Server) sizeJSON(p Photo, label string, w, h int) map[string]any {
	return map[string]any{"label": label, "width": w, "height": h, "source": "http://localhost:8088/images/" + p.ID + ".jpg", "media": "photo"}
}

func (s *Server) albumJSON(a Album) map[string]any {
	return map[string]any{"id": a.ID, "owner": a.Owner, "username": s.person(a.Owner).Username, "primary": first(a.Photos), "secret": "seedsecret", "server": "1", "farm": 1, "photos": len(a.Photos), "videos": 0, "count_views": 0, "date_create": 1700000000, "date_update": 1700000100, "title": content(a.Title), "description": content("Seed album")}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func parsePositive(v string, fallback int) int {
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 {
		return fallback
	}
	return n
}

func (s *Server) requestToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	s.oauth(w, r, "oauth_token=request-token&oauth_token_secret=request-secret&oauth_callback_confirmed=true")
	if r.Method == http.MethodGet || r.Method == http.MethodPost {
		s.mu.Lock()
		s.callbacks["request-token"] = r.Form.Get("oauth_callback")
		s.mu.Unlock()
	}
}

func (s *Server) accessToken(w http.ResponseWriter, r *http.Request) {
	if s.scenarios.revoked(r.FormValue("oauth_token")) {
		http.Error(w, "oauth_problem=token_rejected", http.StatusUnauthorized)
		return
	}
	s.oauth(w, r, "oauth_token=access-token&oauth_token_secret=access-secret&user_nsid=10000000%40N00&username=seed-user&fullname=Seed+User")
}

func (s *Server) oauth(w http.ResponseWriter, r *http.Request, body string) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.scenarios.before(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = w.Write([]byte(body))
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("oauth_token")
	s.mu.Lock()
	callback := s.callbacks[token]
	s.mu.Unlock()
	if callback == "" {
		http.Error(w, "unknown request token", http.StatusBadRequest)
		return
	}
	callbackURL, err := url.Parse(callback)
	if err != nil || (callbackURL.Scheme != "http" && callbackURL.Scheme != "https") {
		http.Error(w, "invalid OAuth callback", http.StatusBadRequest)
		return
	}
	query := callbackURL.Query()
	query.Set("oauth_token", token)
	query.Set("oauth_verifier", "fake-verifier")
	callbackURL.RawQuery = query.Encode()
	http.Redirect(w, r, callbackURL.String(), http.StatusFound)
}

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/images/"), ".jpg")
	found := false
	for _, p := range s.dataset.Photos {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(seedJPEG)))
	if r.Method == http.MethodGet {
		_, _ = w.Write(seedJPEG)
	}
}
