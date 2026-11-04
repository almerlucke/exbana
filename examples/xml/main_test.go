package main

import "testing"

func TestParseTree(t *testing.T) {
	n, err := Parse(`<root id="1" kind="x"><item name="foo">Hello</item><item name="bar" empty="true"/><nested><child>Deep</child></nested></root>`)
	if err != nil {
		t.Fatal(err)
	}

	if n.Name != "root" || len(n.Attrs) != 2 || n.Attrs[0] != (Attr{"id", "1"}) || n.Attrs[1] != (Attr{"kind", "x"}) {
		t.Fatalf("root: %+v", n)
	}

	if len(n.Children) != 3 {
		t.Fatalf("children: %d", len(n.Children))
	}

	if c := n.Children[0]; c.Name != "item" || c.Content != "Hello" || len(c.Children) != 0 {
		t.Errorf("first item: %+v", c)
	}

	if c := n.Children[1]; c.Name != "item" || len(c.Attrs) != 2 || c.Content != "" {
		t.Errorf("second item: %+v", c)
	}

	if c := n.Children[2]; c.Name != "nested" || len(c.Children) != 1 || c.Children[0].Content != "Deep" {
		t.Errorf("nested: %+v", c)
	}
}

func TestParseRejectsInvalidXML(t *testing.T) {
	for _, in := range []string{``, `<a>`, `<a></b>`, `<a b=1/>`, `<a/><b/>`, `text`} {
		if n, err := Parse(in); err == nil {
			t.Errorf("%q: expected an error, got %+v", in, n)
		}
	}
}
