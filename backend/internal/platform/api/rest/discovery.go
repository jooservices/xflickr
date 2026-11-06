package rest

// Discovery exposes only targets supplied by the composition root. It never
// invents routes for later migration slices or checks an external provider.
func Discovery(collections map[string]string) map[string]any {
	links := Links{"self": NewLink("/api/v1", "GET")}
	for relation, href := range collections {
		links[relation] = NewLink(href, "GET")
	}
	return map[string]any{"links": links}
}
