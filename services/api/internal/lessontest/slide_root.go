package lessontest

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// validSlideRoot checks the body fragment the player receives. The engine puts its
// region stylesheet before the slide root (older slides carry it after).
func validSlideRoot(document string) bool {
	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(document), context)
	if err != nil {
		return false
	}
	foundRoot, foundStyle := false, false
	for _, node := range nodes {
		switch node.Type {
		case html.CommentNode:
			continue
		case html.TextNode:
			if strings.TrimSpace(node.Data) != "" {
				return false
			}
		case html.ElementNode:
			if !foundRoot && node.Data == "div" {
				for _, attr := range node.Attr {
					if attr.Key == "id" && attr.Val == "root" {
						foundRoot = true
						break
					}
				}
				if !foundRoot {
					return false
				}
			} else if !foundStyle && node.Data == "style" {
				marked := false
				for _, attr := range node.Attr {
					if attr.Key == "data-duolify-regions" {
						marked = true
					}
				}
				if !marked {
					return false
				}
				foundStyle = true
			} else {
				return false
			}
		default:
			return false
		}
	}
	return foundRoot
}
