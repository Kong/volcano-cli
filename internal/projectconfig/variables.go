package projectconfig

// variableSections are the manifest sections that set project variables and
// which of them functions and frontends receive.
var variableSections = []string{"variables", "shared_variables", "frontend_shared_variables"}

// DeclaresVariables reports whether the manifest declares the variables
// section, which replaces every project variable when applied.
func (m *Manifest) DeclaresVariables() bool {
	return m != nil && m.Variables != nil
}

// VariablesOnly returns a manifest with only m's variables, shared_variables,
// and frontend_shared_variables sections, so they can be applied before code
// that reads them is deployed. ok is false when m declares none of them.
func (m *Manifest) VariablesOnly() (subset *Manifest, ok bool) {
	if m == nil || (m.Variables == nil && m.SharedVariables == nil && m.FrontendSharedVariables == nil) {
		return nil, false
	}
	subset = &Manifest{
		Version:                 m.Version,
		Variables:               m.Variables,
		SharedVariables:         m.SharedVariables,
		FrontendSharedVariables: m.FrontendSharedVariables,
		upload:                  map[string]any{"version": m.Version},
	}
	for _, section := range variableSections {
		if value, declared := m.upload[section]; declared {
			subset.upload[section] = value
		}
	}
	return subset, true
}
