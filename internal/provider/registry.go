package provider

// All is the ordered list of all known providers.
// Add a provider = create file + append here.
var All = []Provider{Claude, Codex}

// ByID returns the provider with the given ID, or nil if not found.
func ByID(id string) *Provider {
	for i := range All {
		if All[i].ID == id {
			return &All[i]
		}
	}
	return nil
}
