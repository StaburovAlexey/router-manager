package templates

import (
	"bytes"
	"text/template"
)

func Render(name string, data any) ([]byte, error) {
	raw, err := FS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	tpl, err := template.New(name).Funcs(template.FuncMap{
		"bool01": func(value bool) int {
			if value {
				return 1
			}
			return 0
		},
	}).Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
