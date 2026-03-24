package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// graphNode uniquely identifies a node in the dependency graph.
type graphNode struct {
	Name string
	URI  protocol.DocumentUri
	Kind protocol.SymbolKind
}

// nodeKey produces a stable string key for deduplication.
func (n graphNode) nodeKey() string {
	return fmt.Sprintf("%s|%s", n.URI, n.Name)
}

// graphEdge represents a directed edge from caller to callee.
type graphEdge struct {
	From string // nodeKey
	To   string // nodeKey
}

// dependencyGraph accumulates nodes and edges during traversal.
type dependencyGraph struct {
	nodes map[string]graphNode // key -> node
	edges []graphEdge
	seen  map[string]bool // nodeKey -> already expanded
}

func newDependencyGraph() *dependencyGraph {
	return &dependencyGraph{
		nodes: make(map[string]graphNode),
		edges: make([]graphEdge, 0),
		seen:  make(map[string]bool),
	}
}

func (g *dependencyGraph) addNode(n graphNode) string {
	key := n.nodeKey()
	if _, ok := g.nodes[key]; !ok {
		g.nodes[key] = n
	}
	return key
}

func (g *dependencyGraph) addEdge(from, to string) {
	for _, e := range g.edges {
		if e.From == from && e.To == to {
			return
		}
	}
	g.edges = append(g.edges, graphEdge{From: from, To: to})
}

// mermaidIDMap assigns short, stable Mermaid node identifiers.
type mermaidIDMap struct {
	ids map[string]string
	seq int
}

func newMermaidIDMap() *mermaidIDMap {
	return &mermaidIDMap{ids: make(map[string]string)}
}

func (m *mermaidIDMap) get(key string) string {
	if id, ok := m.ids[key]; ok {
		return id
	}
	id := fmt.Sprintf("n%d", m.seq)
	m.seq++
	m.ids[key] = id
	return id
}

// mermaidShape returns the mermaid node declaration with shape based on kind.
func mermaidShape(id, label string, kind protocol.SymbolKind) string {
	// Escape quotes in labels
	label = strings.ReplaceAll(label, "\"", "#quot;")
	switch kind {
	case protocol.Interface:
		return fmt.Sprintf("    %s([\"fa:fa-plug %s\"])", id, label)
	case protocol.Class, protocol.Struct:
		return fmt.Sprintf("    %s[\"%s\"]", id, label)
	case protocol.Method:
		return fmt.Sprintf("    %s([\"%s\"])", id, label)
	case protocol.Function, protocol.Constructor:
		return fmt.Sprintf("    %s(\"%s\")", id, label)
	default:
		return fmt.Sprintf("    %s(\"%s\")", id, label)
	}
}

// nodeLabel produces a short display label for a node.
func nodeLabel(n graphNode) string {
	file := RelativePath(strings.TrimPrefix(string(n.URI), "file://"))
	kind := protocol.TableKindMap[n.Kind]
	if kind == "" {
		kind = "Symbol"
	}
	return fmt.Sprintf("%s\\n(%s in %s)", n.Name, kind, file)
}

// GenerateCallGraph builds a mermaid call graph starting from a symbol at a position.
// direction: "outgoing", "incoming", or "both".
func GenerateCallGraph(ctx context.Context, client *lsp.Client, filePath string, line, column, depth int, direction string) (string, error) {
	items, err := prepareCallHierarchy(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "No call hierarchy item found at this position", nil
	}

	g := newDependencyGraph()

	for _, item := range items {
		root := graphNode{
			Name: item.Name,
			URI:  item.URI,
			Kind: item.Kind,
		}
		rootKey := g.addNode(root)

		switch direction {
		case "outgoing":
			if err := walkOutgoing(ctx, client, g, item, rootKey, depth); err != nil {
				return "", err
			}
		case "incoming":
			if err := walkIncoming(ctx, client, g, item, rootKey, depth); err != nil {
				return "", err
			}
		default: // "both"
			if err := walkOutgoing(ctx, client, g, item, rootKey, depth); err != nil {
				return "", err
			}
			// Reset seen for incoming walk from the same root
			g.seen = make(map[string]bool)
			if err := walkIncoming(ctx, client, g, item, rootKey, depth); err != nil {
				return "", err
			}
		}
	}

	return renderMermaid(g, direction), nil
}

func walkOutgoing(ctx context.Context, client *lsp.Client, g *dependencyGraph, item protocol.CallHierarchyItem, parentKey string, remaining int) error {
	if remaining <= 0 {
		return nil
	}
	if g.seen[parentKey] {
		return nil
	}
	g.seen[parentKey] = true

	outgoing, err := client.OutgoingCalls(ctx, protocol.CallHierarchyOutgoingCallsParams{
		Item: item,
	})
	if err != nil {
		if strings.Contains(err.Error(), "code: -32601") {
			// Server doesn't support outgoing calls natively; try synthetic fallback
			synthetic, synthErr := syntheticOutgoingCalls(ctx, client, item)
			if synthErr != nil {
				toolsLogger.Warn("synthetic outgoing calls failed: %v", synthErr)
				return nil
			}
			outgoing = synthetic
		} else {
			toolsLogger.Warn("outgoing calls failed: %v", err)
			return nil
		}
	}

	for _, call := range outgoing {
		child := graphNode{
			Name: call.To.Name,
			URI:  call.To.URI,
			Kind: call.To.Kind,
		}
		childKey := g.addNode(child)
		g.addEdge(parentKey, childKey)

		if err := walkOutgoing(ctx, client, g, call.To, childKey, remaining-1); err != nil {
			return err
		}
	}
	return nil
}

func walkIncoming(ctx context.Context, client *lsp.Client, g *dependencyGraph, item protocol.CallHierarchyItem, childKey string, remaining int) error {
	if remaining <= 0 {
		return nil
	}
	if g.seen[childKey] {
		return nil
	}
	g.seen[childKey] = true

	incoming, err := client.IncomingCalls(ctx, protocol.CallHierarchyIncomingCallsParams{
		Item: item,
	})
	if err != nil {
		toolsLogger.Warn("incoming calls failed: %v", err)
		return nil
	}

	for _, call := range incoming {
		parent := graphNode{
			Name: call.From.Name,
			URI:  call.From.URI,
			Kind: call.From.Kind,
		}
		parentKey := g.addNode(parent)
		g.addEdge(parentKey, childKey)

		if err := walkIncoming(ctx, client, g, call.From, parentKey, remaining-1); err != nil {
			return err
		}
	}
	return nil
}

func renderMermaid(g *dependencyGraph, direction string) string {
	if len(g.nodes) == 0 {
		return "No dependencies found."
	}

	var b strings.Builder
	idMap := newMermaidIDMap()

	graphDir := "TD" // top-down
	if direction == "incoming" {
		graphDir = "BT" // bottom-up for callers
	}

	b.WriteString(fmt.Sprintf("```mermaid\ngraph %s\n", graphDir))

	// Declare nodes
	for key, node := range g.nodes {
		id := idMap.get(key)
		label := nodeLabel(node)
		b.WriteString(mermaidShape(id, label, node.Kind))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Declare edges
	for _, edge := range g.edges {
		fromID := idMap.get(edge.From)
		toID := idMap.get(edge.To)
		b.WriteString(fmt.Sprintf("    %s --> %s\n", fromID, toID))
	}

	b.WriteString("```\n")
	return b.String()
}
