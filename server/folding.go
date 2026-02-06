package server

import (
	"context"

	"github.com/alexispurslane/go-org/org"
	"go.lsp.dev/protocol"
)

// FoldingRanges implements textDocument/foldingRange.
//
// Returns foldable regions for headings, blocks, and drawers in the document.
// Headings use Comment kind, blocks and drawers use Region kind.
func (s *ServerImpl) FoldingRanges(ctx context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	if serverState == nil {
		return nil, nil
	}

	serverState.Mu.RLock()
	defer serverState.Mu.RUnlock()

	doc, ok := serverState.OpenDocs[params.TextDocument.URI]
	if !ok {
		return nil, nil
	}

	return findFoldingRanges(doc), nil
}

// findFoldingRanges extracts all collapsible regions from an org document.
//
// Uses go-org's Position() method which returns StartLine/EndLine covering
// the full extent of each node. For headings, EndLine extends through the
// entire section. For blocks and drawers, EndLine is the closing delimiter.
func findFoldingRanges(doc *org.Document) []protocol.FoldingRange {
	return collectSectionFoldingRanges(doc.Outline.Children)
}

// collectSectionFoldingRanges recursively collects folding ranges from sections.
func collectSectionFoldingRanges(sections []*org.Section) []protocol.FoldingRange {
	var ranges []protocol.FoldingRange

	for _, section := range sections {
		if section == nil || section.Headline == nil {
			continue
		}

		// Add heading fold
		pos := section.Headline.Position()
		if pos.EndLine > pos.StartLine {
			kind := protocol.CommentFoldingRange
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: uint32(pos.StartLine + 1), // Skip heading line itself
				EndLine:   uint32(pos.EndLine),
				Kind:      kind,
			})
		}

		// Add property drawer fold if present (stored separately from Children)
		if section.Headline.Properties != nil {
			pos := section.Headline.Properties.Pos
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: uint32(pos.StartLine),
				EndLine:   uint32(pos.EndLine),
				Kind:      protocol.RegionFoldingRange,
			})
		}

		// Walk children of this headline for blocks and regular drawers
		section.Headline.Range(func(node org.Node) bool {
			switch n := node.(type) {
			case org.Block:
				pos := n.Position()
				ranges = append(ranges, protocol.FoldingRange{
					StartLine: uint32(pos.StartLine),
					EndLine:   uint32(pos.EndLine),
					Kind:      protocol.RegionFoldingRange,
				})
			case org.Drawer:
				pos := n.Position()
				ranges = append(ranges, protocol.FoldingRange{
					StartLine: uint32(pos.StartLine),
					EndLine:   uint32(pos.EndLine),
					Kind:      protocol.RegionFoldingRange,
				})
			}
			return true
		})

		// Recurse into subsections and append their ranges
		ranges = append(ranges, collectSectionFoldingRanges(section.Children)...)
	}

	return ranges
}
