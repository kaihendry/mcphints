package main

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

var descriptionMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

func renderDescription(value string) template.HTML {
	source := []byte(value)
	doc := descriptionMarkdown.Parser().Parse(text.NewReader(source))
	var images []*ast.Image
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch n := node.(type) {
			case *ast.Heading:
				n.Level = min(6, n.Level+4) // stay below the Description heading
			case *ast.Image:
				images = append(images, n)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, image := range images {
		image.Parent().ReplaceChild(image.Parent(), image, ast.NewString(image.Text(source)))
	}
	var out bytes.Buffer
	if err := descriptionMarkdown.Renderer().Render(&out, source, doc); err != nil {
		return template.HTML(template.HTMLEscapeString(value))
	}
	// Goldmark's default renderer omits raw HTML and dangerous links. Image
	// nodes become alt text above, so reviewing a description fetches nothing.
	return template.HTML(out.String())
}
