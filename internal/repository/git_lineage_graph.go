package repository

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/zhao-xuan/PixLog/internal/recipe"
)

type ProvenanceGraph struct {
	Asset   string                `json:"asset"`
	Nodes   []ProvenanceGraphNode `json:"nodes"`
	Edges   []ProvenanceGraphEdge `json:"edges"`
	Missing []string              `json:"missing,omitempty"`
}

type ProvenanceGraphNode struct {
	ID       string `json:"id"`
	OID      string `json:"oid,omitempty"`
	Kind     string `json:"kind"`
	Label    string `json:"label,omitempty"`
	Exists   bool   `json:"exists"`
	Verified bool   `json:"verified"`
}

type ProvenanceGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Role string `json:"role"`
	Path string `json:"path,omitempty"`
}

func (g *GitRepository) ProvenanceGraph(assetPath string, hydrate bool) (ProvenanceGraph, error) {
	history, err := g.Lineage(assetPath)
	if err != nil {
		return ProvenanceGraph{}, err
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return ProvenanceGraph{}, err
	}
	nodes := map[string]ProvenanceGraphNode{}
	edges := map[string]ProvenanceGraphEdge{}
	missing := map[string]struct{}{}
	processedRecipes := map[string]bool{}

	addNode := func(node ProvenanceGraphNode) {
		if existing, found := nodes[node.ID]; found {
			node.Exists = node.Exists || existing.Exists
			node.Verified = node.Verified || existing.Verified
			if node.Label == "" {
				node.Label = existing.Label
			}
		}
		nodes[node.ID] = node
	}
	addEdge := func(edge ProvenanceGraphEdge) {
		key := edge.From + "\x00" + edge.To + "\x00" + edge.Role + "\x00" + edge.Path
		edges[key] = edge
	}
	var processRecipe func(string) error
	processRecipe = func(recipeOID string) error {
		if processedRecipes[recipeOID] {
			return nil
		}
		processedRecipes[recipeOID] = true
		data, loadErr := g.graphObject(store, recipeOID, hydrate)
		if loadErr != nil {
			addNode(ProvenanceGraphNode{ID: recipeOID, OID: recipeOID, Kind: "recipe"})
			missing[recipeOID] = struct{}{}
			return nil
		}
		addNode(ProvenanceGraphNode{ID: recipeOID, OID: recipeOID, Kind: "recipe", Exists: true, Verified: true})
		references, err := recipe.References(data)
		if err != nil {
			return fmt.Errorf("read references from recipe %s: %w", recipeOID, err)
		}
		for _, reference := range references {
			referenceData, referenceErr := g.graphObject(store, reference.OID, hydrate)
			exists := referenceErr == nil
			addNode(ProvenanceGraphNode{
				ID: reference.OID, OID: reference.OID, Kind: reference.Kind,
				Exists: exists, Verified: exists,
			})
			addEdge(ProvenanceGraphEdge{From: recipeOID, To: reference.OID, Role: reference.Role, Path: reference.Path})
			if !exists {
				missing[reference.OID] = struct{}{}
				continue
			}
			if reference.Kind == "recipe" {
				if err := processRecipe(reference.OID); err != nil {
					return err
				}
			}
			_ = referenceData
		}
		return nil
	}

	for _, version := range history {
		commitID := "git:" + version.CommitOID
		addNode(ProvenanceGraphNode{ID: commitID, OID: version.CommitOID, Kind: "git-commit", Label: version.Message, Exists: true, Verified: true})
		if version.ContentOID != "" {
			addNode(ProvenanceGraphNode{ID: version.ContentOID, OID: version.ContentOID, Kind: "content", Exists: true, Verified: true})
			addEdge(ProvenanceGraphEdge{From: commitID, To: version.ContentOID, Role: "version"})
		}
		if version.RecipeOID != "" {
			addEdge(ProvenanceGraphEdge{From: version.RecipeOID, To: version.ContentOID, Role: "produced"})
			if err := processRecipe(version.RecipeOID); err != nil {
				return ProvenanceGraph{}, err
			}
		}
	}

	graph := ProvenanceGraph{Asset: assetPath}
	for _, node := range nodes {
		graph.Nodes = append(graph.Nodes, node)
	}
	for _, edge := range edges {
		graph.Edges = append(graph.Edges, edge)
	}
	for oid := range missing {
		graph.Missing = append(graph.Missing, oid)
	}
	sort.Slice(graph.Nodes, func(left, right int) bool { return graph.Nodes[left].ID < graph.Nodes[right].ID })
	sort.Slice(graph.Edges, func(left, right int) bool {
		if graph.Edges[left].From == graph.Edges[right].From {
			if graph.Edges[left].To == graph.Edges[right].To {
				return graph.Edges[left].Role < graph.Edges[right].Role
			}
			return graph.Edges[left].To < graph.Edges[right].To
		}
		return graph.Edges[left].From < graph.Edges[right].From
	})
	sort.Strings(graph.Missing)
	return graph, nil
}

func (g *GitRepository) graphObject(store *GitMediaStore, oid string, hydrate bool) ([]byte, error) {
	if hydrate {
		return g.loadMediaObject(store, oid)
	}
	data, err := store.Get(oid)
	if errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return data, err
}
