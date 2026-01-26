package org

import (
	"math"
	"regexp"
	"strings"
)

type Paragraph struct {
	Children []Node
	Pos      Position
}

type HorizontalRule struct {
	Pos Position
}

var horizontalRuleRegexp = regexp.MustCompile(`^(\s*)-{5,}\s*$`)
var plainTextRegexp = regexp.MustCompile(`^(\s*)(.*)`)

func lexText(line string) (token, bool) {
	if m := plainTextRegexp.FindStringSubmatch(line); m != nil {
		return token{kind: "text", lvl: len(m[1]), content: m[2], matches: m}, true
	}
	return nilToken, false
}

func lexHorizontalRule(line string) (token, bool) {
	if m := horizontalRuleRegexp.FindStringSubmatch(line); m != nil {
		return token{kind: "horizontalRule", lvl: len(m[1]), content: "", matches: m}, true
	}
	return nilToken, false
}

func (d *Document) parseParagraph(i int, parentStop stopFn) (int, Node) {
	lines, start := []string{d.tokens[i].content}, i
	stop := func(d *Document, i int) bool {
		return parentStop(d, i) || d.tokens[i].kind != "text" || d.tokens[i].content == ""
	}
	for i += 1; !stop(d, i); i++ {
		lvl := math.Max(float64(d.tokens[i].lvl-d.baseLvl), 0)
		lines = append(lines, strings.Repeat(" ", int(lvl))+d.tokens[i].content)
	}
	consumed := i - start
	paragraph := Paragraph{Children: d.parseInline(strings.Join(lines, "\n"))}
	endToken := d.tokens[i-1]
	paragraph.Pos = Position{
		StartLine:   d.tokens[start].line,
		StartColumn: d.tokens[start].startCol,
		EndLine:     endToken.line,
		EndColumn:   endToken.endCol,
	}
	return consumed, paragraph
}

func (d *Document) parseHorizontalRule(i int, parentStop stopFn) (int, Node) {
	t := d.tokens[i]
	hr := HorizontalRule{}
	hr.Pos = Position{
		StartLine:   t.line,
		StartColumn: t.startCol,
		EndLine:     t.line,
		EndColumn:   t.endCol,
	}
	return 1, hr
}

func (n Paragraph) String() string      { return String(n) }
func (n HorizontalRule) String() string { return String(n) }
