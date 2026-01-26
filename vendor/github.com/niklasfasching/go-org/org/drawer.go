package org

import (
	"regexp"
	"strings"
)

type Drawer struct {
	Name     string
	Children []Node
	Pos      Position
}

type PropertyDrawer struct {
	Properties [][]string
	Pos        Position
}

var beginDrawerRegexp = regexp.MustCompile(`^(\s*):(\S+):\s*$`)
var endDrawerRegexp = regexp.MustCompile(`(?i)^(\s*):END:\s*$`)
var propertyRegexp = regexp.MustCompile(`^(\s*):(\S+):(\s+(.*)$|$)`)

func lexDrawer(line string) (token, bool) {
	if m := endDrawerRegexp.FindStringSubmatch(line); m != nil {
		return token{kind: "endDrawer", lvl: len(m[1]), content: "", matches: m}, true
	} else if m := beginDrawerRegexp.FindStringSubmatch(line); m != nil {
		return token{kind: "beginDrawer", lvl: len(m[1]), content: strings.ToUpper(m[2]), matches: m}, true
	}
	return nilToken, false
}

func (d *Document) parseDrawer(i int, parentStop stopFn) (int, Node) {
	name := strings.ToUpper(d.tokens[i].content)
	if name == "PROPERTIES" {
		return d.parsePropertyDrawer(i, parentStop)
	}
	drawer, start := Drawer{Name: name}, i
	drawer.Pos = Position{
		StartLine:   d.tokens[start].line,
		StartColumn: d.tokens[start].startCol,
	}
	i++
	stop := func(d *Document, i int) bool {
		if parentStop(d, i) {
			return true
		}
		kind := d.tokens[i].kind
		return kind == "beginDrawer" || kind == "endDrawer" || kind == "headline"
	}
	for {
		consumed, nodes := d.parseMany(i, stop)
		i += consumed
		drawer.Children = append(drawer.Children, nodes...)
		if i < len(d.tokens) && d.tokens[i].kind == "beginDrawer" {
			p := Paragraph{Children: []Node{Text{Content: ":" + d.tokens[i].content + ":", IsRaw: false}}, Pos: Position{}}
			drawer.Children = append(drawer.Children, p)
			i++
		} else {
			break
		}
	}
	if i < len(d.tokens) && d.tokens[i].kind == "endDrawer" {
		i++
	}
	return i - start, drawer
}

func (d *Document) parsePropertyDrawer(i int, parentStop stopFn) (int, Node) {
	drawer, start := PropertyDrawer{}, i
	drawer.Pos = Position{
		StartLine:   d.tokens[start].line,
		StartColumn: d.tokens[start].startCol,
	}
	i++
	stop := func(d *Document, i int) bool {
		return parentStop(d, i) || (d.tokens[i].kind != "text" && d.tokens[i].kind != "beginDrawer")
	}
	for ; !stop(d, i); i++ {
		m := propertyRegexp.FindStringSubmatch(d.tokens[i].matches[0])
		if m == nil {
			return 0, nil
		}
		k, v := strings.ToUpper(m[2]), strings.TrimSpace(m[4])
		drawer.Properties = append(drawer.Properties, []string{k, v})
	}
	if i < len(d.tokens) && d.tokens[i].kind == "endDrawer" {
		i++
	} else {
		return 0, nil
	}
	if start < i && i-1 < len(d.tokens) {
		drawer.Pos.EndLine = d.tokens[i-1].line
		drawer.Pos.EndColumn = d.tokens[i-1].endCol
	}
	return i - start, drawer
}

func (d *PropertyDrawer) Get(key string) (string, bool) {
	if d == nil {
		return "", false
	}
	for _, kvPair := range d.Properties {
		if kvPair[0] == key {
			return kvPair[1], true
		}
	}
	return "", false
}

func (n Drawer) String() string         { return String(n) }
func (n PropertyDrawer) String() string { return String(n) }
