package projectconfig

// SchedulersOnly returns a manifest with one entry per named function that the
// manifests list schedulers for, holding just its name, kind, and schedulers.
// The server keeps every section and field a manifest leaves out, so applying
// it changes only those schedulers. When several manifests list a function's
// schedulers, the last one wins, as when they apply in order. ok is false when
// none does.
func SchedulersOnly(manifests []*Manifest, names map[string]bool) (subset *Manifest, ok bool) {
	var order []string
	typed := map[string]FunctionManifest{}
	upload := map[string]map[string]any{}
	for _, m := range manifests {
		if m == nil || m.Functions == nil {
			continue
		}
		for _, fn := range *m.Functions {
			if !names[fn.Name] || fn.Schedulers == nil {
				continue
			}
			if _, seen := typed[fn.Name]; !seen {
				order = append(order, fn.Name)
			}
			typed[fn.Name] = FunctionManifest{Name: fn.Name, Kind: fn.Kind, Schedulers: fn.Schedulers}
			upload[fn.Name] = uploadedSchedulers(m, fn.Name)
		}
	}
	if len(order) == 0 {
		return nil, false
	}
	functions := make([]FunctionManifest, 0, len(order))
	entries := make([]any, 0, len(order))
	for _, name := range order {
		functions = append(functions, typed[name])
		entries = append(entries, upload[name])
	}
	return &Manifest{
		Version:   ManifestVersion,
		Functions: &functions,
		upload:    map[string]any{"version": ManifestVersion, "functions": entries},
	}, true
}

// uploadedSchedulers returns the name, kind, and schedulers of a function's
// entry in m's upload body.
func uploadedSchedulers(m *Manifest, name string) map[string]any {
	entry := map[string]any{"name": name}
	entries, _ := m.upload["functions"].([]any)
	for _, e := range entries {
		fields, ok := e.(map[string]any)
		if !ok || fields["name"] != name {
			continue
		}
		for _, key := range []string{"kind", "schedulers"} {
			if value, declared := fields[key]; declared {
				entry[key] = value
			}
		}
	}
	return entry
}
