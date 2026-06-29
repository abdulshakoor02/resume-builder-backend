package generator

type Template struct {
	Name        string
	Label       string
	Description string
}

func GetTemplates() []Template {
	return []Template{
		{
			Name:        "classic",
			Label:       "Classic",
			Description: "Traditional resume layout with clean serif styling. Best for corporate and conservative industries.",
		},
		{
			Name:        "modern",
			Label:       "Modern",
			Description: "Bold colored header with clean sans-serif body. Great for tech, design, and creative roles.",
		},
		{
			Name:        "minimal",
			Label:       "Minimal",
			Description: "Ultra-clean with generous whitespace and thin separators. Ideal for senior roles and consulting.",
		},
	}
}

func GetTemplate(name string) *Template {
	for _, t := range GetTemplates() {
		if t.Name == name {
			return &t
		}
	}
	return nil
}
