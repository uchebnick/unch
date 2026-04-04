//go:build cgo

package indexing

import (
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type treeSitterSpec struct {
	language func() *sitter.Language
	extract  func([]byte, *sitter.Node) []IndexedSymbol
}

var treeSitterSpecs = map[string]treeSitterSpec{
	".go":  {language: func() *sitter.Language { return sitter.NewLanguage(tsgo.Language()) }, extract: extractGoSymbols},
	".js":  {language: func() *sitter.Language { return sitter.NewLanguage(tsjavascript.Language()) }, extract: extractJSSymbols},
	".jsx": {language: func() *sitter.Language { return sitter.NewLanguage(tsjavascript.Language()) }, extract: extractJSSymbols},
	".mjs": {language: func() *sitter.Language { return sitter.NewLanguage(tsjavascript.Language()) }, extract: extractJSSymbols},
	".cjs": {language: func() *sitter.Language { return sitter.NewLanguage(tsjavascript.Language()) }, extract: extractJSSymbols},
	".ts":  {language: func() *sitter.Language { return sitter.NewLanguage(tstypescript.LanguageTypescript()) }, extract: extractTSSymbols},
	".tsx": {language: func() *sitter.Language { return sitter.NewLanguage(tstypescript.LanguageTSX()) }, extract: extractTSSymbols},
	".py":  {language: func() *sitter.Language { return sitter.NewLanguage(tspython.Language()) }, extract: extractPythonSymbols},
}

// extractTreeSitterSymbols parses the file with the matching grammar and
// returns symbols only when the parse completed without syntax errors.
func extractTreeSitterSymbols(path string, source []byte) ([]IndexedSymbol, bool) {
	spec, ok := treeSitterSpecs[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return nil, false
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(spec.language()); err != nil {
		return nil, false
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, false
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil || root.HasError() {
		return nil, false
	}

	return spec.extract(source, root), true
}

// extractGoSymbols indexes top-level Go declarations and expands interface
// methods into separate search entries.
func extractGoSymbols(source []byte, root *sitter.Node) []IndexedSymbol {
	children := nodeChildren(root)
	fileContext := ""
	for idx, child := range children {
		if child.Kind() == "package_clause" {
			fileContext = extractLeadingDoc(children, idx, source)
			break
		}
	}

	var symbols []IndexedSymbol
	for idx := range children {
		child := &children[idx]
		switch child.Kind() {
		case "function_declaration":
			symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
				Kind:        "function",
				Name:        nodeFieldText(child, "name", source),
				FileContext: fileContext,
				Doc:         combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source)),
			}))
		case "method_declaration":
			container := goReceiverName(child, source)
			symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
				Kind:        "method",
				Name:        nodeFieldText(child, "name", source),
				Container:   container,
				FileContext: fileContext,
				Doc:         combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source)),
			}))
		case "type_declaration":
			symbols = append(symbols, extractGoTypeDeclarationSymbols(child, source, fileContext, extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source))...)
		case "const_declaration":
			symbols = append(symbols, extractGoValueSymbols(child, source, fileContext, "constant", extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source))...)
		case "var_declaration":
			symbols = append(symbols, extractGoValueSymbols(child, source, fileContext, "variable", extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source))...)
		}
	}

	return symbols
}

func extractGoTypeDeclarationSymbols(node *sitter.Node, source []byte, fileContext string, leadingDoc string, trailing string) []IndexedSymbol {
	var symbols []IndexedSymbol
	typeSpecs := childNodesByKind(node, "type_spec")
	docText := combineDocText(leadingDoc, trailing)

	for _, spec := range typeSpecs {
		typeNode := firstNamedChildOfKinds(&spec, "struct_type", "interface_type")
		kind := "type"
		if typeNode != nil {
			switch typeNode.Kind() {
			case "struct_type":
				kind = "struct"
			case "interface_type":
				kind = "interface"
			}
		}

		specNode := spec
		name := nodeFieldText(&specNode, "name", source)
		symbols = append(symbols, newSymbolFromNode(&specNode, source, symbolMeta{
			Kind:        kind,
			Name:        name,
			FileContext: fileContext,
			Doc:         docText,
		}))

		if typeNode != nil && typeNode.Kind() == "interface_type" {
			container := name
			children := nodeChildren(typeNode)
			for idx := range children {
				child := &children[idx]
				if child.Kind() != "method_elem" {
					continue
				}
				symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
					Kind:        "method",
					Name:        nodeFieldText(child, "name", source),
					Container:   container,
					FileContext: fileContext,
					Doc:         combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source)),
				}))
			}
		}
	}

	return symbols
}

func extractGoValueSymbols(node *sitter.Node, source []byte, fileContext string, kind string, leadingDoc string, trailing string) []IndexedSymbol {
	var symbols []IndexedSymbol
	docText := combineDocText(leadingDoc, trailing)

	specKinds := []string{"const_spec", "var_spec"}
	for _, specKind := range specKinds {
		for _, spec := range childNodesByKind(node, specKind) {
			names := childNodesByKind(&spec, "identifier")
			for _, nameNode := range names {
				text := normalizeText(nameNode.Utf8Text(source))
				if text == "" {
					continue
				}
				specNode := spec
				symbols = append(symbols, newSymbolFromNode(&specNode, source, symbolMeta{
					Kind:        kind,
					Name:        text,
					FileContext: fileContext,
					Doc:         docText,
				}))
			}
		}
	}

	return symbols
}

func extractJSSymbols(source []byte, root *sitter.Node) []IndexedSymbol {
	return extractJSLikeSymbols(source, root, false)
}

func extractTSSymbols(source []byte, root *sitter.Node) []IndexedSymbol {
	return extractJSLikeSymbols(source, root, true)
}

func extractJSLikeSymbols(source []byte, root *sitter.Node, isTypeScript bool) []IndexedSymbol {
	children := nodeChildren(root)
	var symbols []IndexedSymbol

	for idx := range children {
		child := &children[idx]
		docText := combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source))

		switch child.Kind() {
		case "export_statement":
			if decl := firstNamedNonCommentChild(child); decl != nil {
				symbols = append(symbols, extractJSTopLevelSymbols(decl, source, docText, "", isTypeScript)...)
			}
		default:
			symbols = append(symbols, extractJSTopLevelSymbols(child, source, docText, "", isTypeScript)...)
		}
	}

	return symbols
}

func extractJSTopLevelSymbols(node *sitter.Node, source []byte, docText string, fileContext string, isTypeScript bool) []IndexedSymbol {
	switch node.Kind() {
	case "class_declaration", "class":
		return extractJSClassSymbols(node, source, docText, fileContext)
	case "abstract_class_declaration":
		return extractJSClassSymbols(node, source, docText, fileContext)
	case "function_declaration", "generator_function_declaration":
		return []IndexedSymbol{newSymbolFromNode(node, source, symbolMeta{
			Kind:        "function",
			Name:        nodeFieldText(node, "name", source),
			FileContext: fileContext,
			Doc:         docText,
		})}
	case "interface_declaration":
		return extractTSInterfaceSymbols(node, source, docText, fileContext)
	case "enum_declaration":
		return []IndexedSymbol{newSymbolFromNode(node, source, symbolMeta{
			Kind:        "enum",
			Name:        firstNamedIdentifierText(node, source),
			FileContext: fileContext,
			Doc:         docText,
		})}
	case "type_alias_declaration":
		return []IndexedSymbol{newSymbolFromNode(node, source, symbolMeta{
			Kind:        "type",
			Name:        nodeFieldText(node, "name", source),
			FileContext: fileContext,
			Doc:         docText,
		})}
	case "lexical_declaration", "variable_declaration":
		return extractJSVariableSymbols(node, source, docText, fileContext)
	default:
		if isTypeScript && node.Kind() == "function_signature" {
			return []IndexedSymbol{newSymbolFromNode(node, source, symbolMeta{
				Kind:        "function",
				Name:        nodeFieldText(node, "name", source),
				FileContext: fileContext,
				Doc:         docText,
			})}
		}
	}

	return nil
}

func extractJSClassSymbols(node *sitter.Node, source []byte, docText string, fileContext string) []IndexedSymbol {
	name := nodeFieldText(node, "name", source)
	classSymbol := newSymbolFromNode(node, source, symbolMeta{
		Kind:        "class",
		Name:        name,
		FileContext: fileContext,
		Doc:         docText,
	})

	symbols := []IndexedSymbol{classSymbol}
	body := firstNamedChildOfKinds(node, "class_body")
	if body == nil {
		return symbols
	}

	children := nodeChildren(body)
	for idx := range children {
		child := &children[idx]
		if child.Kind() != "method_definition" {
			continue
		}
		methodName := firstNamedIdentifierText(child, source)
		if methodName == "constructor" {
			continue
		}
		symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
			Kind:        "method",
			Name:        methodName,
			Container:   name,
			FileContext: fileContext,
			Doc:         combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source)),
		}))
	}

	return symbols
}

func extractTSInterfaceSymbols(node *sitter.Node, source []byte, docText string, fileContext string) []IndexedSymbol {
	name := nodeFieldText(node, "name", source)
	symbols := []IndexedSymbol{newSymbolFromNode(node, source, symbolMeta{
		Kind:        "interface",
		Name:        name,
		FileContext: fileContext,
		Doc:         docText,
	})}

	body := firstNamedChildOfKinds(node, "interface_body")
	if body == nil {
		return symbols
	}

	children := nodeChildren(body)
	for idx := range children {
		child := &children[idx]
		switch child.Kind() {
		case "method_signature", "abstract_method_signature":
			symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
				Kind:        "method",
				Name:        firstNamedIdentifierText(child, source),
				Container:   name,
				FileContext: fileContext,
				Doc:         combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source)),
			}))
		}
	}

	return symbols
}

func extractJSVariableSymbols(node *sitter.Node, source []byte, docText string, fileContext string) []IndexedSymbol {
	declarationKind := jsVariableDeclarationKind(node)
	var symbols []IndexedSymbol

	for _, decl := range childNodesByKind(node, "variable_declarator") {
		declNode := decl
		name := nodeFieldText(&declNode, "name", source)
		if name == "" {
			continue
		}

		value := declNode.ChildByFieldName("value")
		kind := declarationKind
		container := ""
		if value != nil {
			switch value.Kind() {
			case "arrow_function", "function_expression", "generator_function", "generator_function_declaration":
				kind = "function"
			case "class", "class_declaration":
				kind = "class"
			}
		}

		symbol := newSymbolFromNode(&declNode, source, symbolMeta{
			Kind:        kind,
			Name:        name,
			Container:   container,
			FileContext: fileContext,
			Doc:         combineDocText(docText, extractTrailingComment(nodeChildren(&declNode), 0, source)),
		})
		if value != nil && (value.Kind() == "arrow_function" || value.Kind() == "function_expression" || value.Kind() == "generator_function") {
			symbol.Body = bodySnippet(value, source)
			symbol.Signature = firstNLines(declNode.Utf8Text(source), 2)
		} else if value != nil && kind == "class" {
			symbol.Body = bodySnippet(value, source)
			symbol.Signature = signatureText(&declNode, source)
		} else if value != nil {
			symbol.Body = firstNLines(value.Utf8Text(source), 6)
		}
		symbols = append(symbols, symbol)
	}

	return symbols
}

func extractPythonSymbols(source []byte, root *sitter.Node) []IndexedSymbol {
	children := nodeChildren(root)
	var symbols []IndexedSymbol

	for idx := range children {
		child := &children[idx]
		switch child.Kind() {
		case "class_definition":
			symbols = append(symbols, extractPythonClassSymbols(child, source, combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source)))...)
		case "function_definition":
			docText := combineDocText(
				extractLeadingDoc(children, idx, source),
				extractTrailingComment(children, idx, source),
				pythonDocstring(child, source),
			)
			symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
				Kind: "function",
				Name: nodeFieldText(child, "name", source),
				Doc:  docText,
			}))
		case "expression_statement":
			if assignment := firstNamedChildOfKinds(child, "assignment"); assignment != nil {
				name := firstNamedIdentifierText(assignment, source)
				if isPythonConstantName(name) {
					symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
						Kind: "constant",
						Name: name,
						Doc:  combineDocText(extractLeadingDoc(children, idx, source), extractTrailingComment(children, idx, source)),
					}))
				}
			}
		}
	}

	return symbols
}

func extractPythonClassSymbols(node *sitter.Node, source []byte, docText string) []IndexedSymbol {
	name := nodeFieldText(node, "name", source)
	classSymbol := newSymbolFromNode(node, source, symbolMeta{
		Kind: "class",
		Name: name,
		Doc:  combineDocText(docText, pythonDocstring(node, source)),
	})

	symbols := []IndexedSymbol{classSymbol}
	body := firstNamedChildOfKinds(node, "block")
	if body == nil {
		return symbols
	}

	children := nodeChildren(body)
	for idx := range children {
		child := &children[idx]
		if child.Kind() != "function_definition" {
			continue
		}
		symbols = append(symbols, newSymbolFromNode(child, source, symbolMeta{
			Kind:      "method",
			Name:      nodeFieldText(child, "name", source),
			Container: name,
			Doc: combineDocText(
				extractLeadingDoc(children, idx, source),
				extractTrailingComment(children, idx, source),
				pythonDocstring(child, source),
			),
		}))
	}

	return symbols
}

type symbolMeta struct {
	Kind        string
	Name        string
	Container   string
	FileContext string
	Doc         string
}

func newSymbolFromNode(node *sitter.Node, source []byte, meta symbolMeta) IndexedSymbol {
	signature := signatureText(node, source)
	body := bodySnippet(node, source)
	qualifiedName := strings.TrimSpace(meta.Name)
	if meta.Container != "" && meta.Name != "" {
		qualifiedName = meta.Container + "." + meta.Name
	}
	if qualifiedName == "" {
		qualifiedName = strings.TrimSpace(firstNamedIdentifierText(node, source))
	}

	return IndexedSymbol{
		Line:          int(node.StartPosition().Row) + 1,
		Kind:          strings.TrimSpace(meta.Kind),
		Name:          strings.TrimSpace(meta.Name),
		Container:     strings.TrimSpace(meta.Container),
		QualifiedName: strings.TrimSpace(qualifiedName),
		Signature:     signature,
		Documentation: strings.TrimSpace(meta.Doc),
		Body:          body,
		FileContext:   strings.TrimSpace(meta.FileContext),
	}
}

func nodeChildren(node *sitter.Node) []sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.Children(cursor)
}

func childNodesByKind(node *sitter.Node, kind string) []sitter.Node {
	var out []sitter.Node
	for _, child := range nodeChildren(node) {
		if child.Kind() == kind {
			out = append(out, child)
		}
	}
	return out
}

func firstNamedNonCommentChild(node *sitter.Node) *sitter.Node {
	for _, child := range nodeChildren(node) {
		if !child.IsNamed() || child.Kind() == "comment" {
			continue
		}
		childCopy := child
		return &childCopy
	}
	return nil
}

func firstNamedChildMatching(node *sitter.Node, kinds map[string]struct{}) *sitter.Node {
	for _, child := range nodeChildren(node) {
		if _, ok := kinds[child.Kind()]; ok {
			childCopy := child
			return &childCopy
		}
	}
	return nil
}

func firstNamedChildOfKinds(node *sitter.Node, kinds ...string) *sitter.Node {
	allowed := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = struct{}{}
	}
	return firstNamedChildMatching(node, allowed)
}

func nodeFieldText(node *sitter.Node, field string, source []byte) string {
	if node == nil {
		return ""
	}
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return normalizeText(child.Utf8Text(source))
}

func firstNamedIdentifierText(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	for _, child := range nodeChildren(node) {
		switch child.Kind() {
		case "identifier", "type_identifier", "field_identifier", "property_identifier", "private_property_identifier":
			return normalizeText(child.Utf8Text(source))
		}
	}
	return ""
}

func signatureText(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	body := declarationBodyNode(node)
	if body != nil && body.StartByte() > node.StartByte() {
		return firstNLines(string(source[node.StartByte():body.StartByte()]), 4)
	}
	return firstNLines(node.Utf8Text(source), 4)
}

func bodySnippet(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	body := declarationBodyNode(node)
	if body != nil {
		return firstNLines(body.Utf8Text(source), 10)
	}
	return ""
}

// declarationBodyNode locates the node that should be treated as the symbol body
// for snippet extraction across different language grammars.
func declarationBodyNode(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}

	for _, field := range []string{"body"} {
		if child := node.ChildByFieldName(field); child != nil {
			return child
		}
	}

	for _, kind := range []string{"block", "statement_block", "class_body", "interface_body", "enum_body", "struct_type", "interface_type"} {
		if child := firstNamedChildOfKinds(node, kind); child != nil {
			return child
		}
	}

	return nil
}

// extractLeadingDoc attaches only the contiguous comment block immediately
// above a declaration. Loose comments separated by blank lines are ignored.
func extractLeadingDoc(children []sitter.Node, idx int, source []byte) string {
	if idx <= 0 || idx >= len(children) {
		return ""
	}

	current := children[idx]
	end := idx - 1
	if children[end].Kind() != "comment" {
		return ""
	}
	if int(children[end].EndPosition().Row)+1 != int(current.StartPosition().Row) {
		return ""
	}

	start := end
	for start-1 >= 0 && children[start-1].Kind() == "comment" {
		prev := children[start-1]
		next := children[start]
		if int(prev.EndPosition().Row)+1 < int(next.StartPosition().Row) {
			break
		}
		start--
	}

	var parts []string
	for i := start; i <= end; i++ {
		text := cleanCommentText(children[i].Utf8Text(source))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// extractTrailingComment captures same-line trailing comments from the
// declaration itself or from child nodes that start on the same line.
func extractTrailingComment(children []sitter.Node, idx int, source []byte) string {
	if idx >= 0 && idx < len(children) {
		current := children[idx]
		for _, child := range nodeChildren(&current) {
			if child.Kind() == "comment" && int(child.StartPosition().Row) == int(current.StartPosition().Row) {
				return cleanCommentText(child.Utf8Text(source))
			}
		}

		if idx+1 < len(children) {
			next := children[idx+1]
			if next.Kind() == "comment" && int(next.StartPosition().Row) == int(current.StartPosition().Row) {
				return cleanCommentText(next.Utf8Text(source))
			}
		}
	}

	return ""
}

func cleanCommentText(raw string) string {
	raw = normalizeText(raw)
	if raw == "" {
		return ""
	}

	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimPrefix(line, "*/")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSuffix(line, "*/")
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func pythonDocstring(node *sitter.Node, source []byte) string {
	body := firstNamedChildOfKinds(node, "block")
	if body == nil {
		return ""
	}

	children := nodeChildren(body)
	if len(children) == 0 {
		return ""
	}

	first := &children[0]
	if first.Kind() != "expression_statement" {
		return ""
	}

	str := firstNamedChildOfKinds(first, "string")
	if str == nil {
		return ""
	}

	return cleanPythonStringLiteral(str.Utf8Text(source))
}

func cleanPythonStringLiteral(raw string) string {
	raw = normalizeText(raw)
	for _, quote := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(raw, quote) && strings.HasSuffix(raw, quote) && len(raw) >= len(quote)*2 {
			raw = strings.TrimPrefix(raw, quote)
			raw = strings.TrimSuffix(raw, quote)
			break
		}
	}

	lines := strings.Split(raw, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func combineDocText(parts ...string) string {
	var filtered []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "\n")
}

func goReceiverName(node *sitter.Node, source []byte) string {
	receiver := node.NamedChild(0)
	if receiver == nil {
		return ""
	}
	text := normalizeText(receiver.Utf8Text(source))
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	text = strings.ReplaceAll(text, "*", "")
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func jsVariableDeclarationKind(node *sitter.Node) string {
	if node == nil {
		return "variable"
	}
	for _, child := range nodeChildren(node) {
		switch child.Kind() {
		case "const":
			return "constant"
		case "let", "var":
			return "variable"
		}
	}
	return "variable"
}

func isPythonConstantName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}
