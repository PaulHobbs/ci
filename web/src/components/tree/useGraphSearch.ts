import { useMemo } from 'react';
import { Graph } from './buildGraph';

interface SearchResult {
  directMatches: Set<string> | null;
  filteredNodeSet: Set<string> | null;
}

/**
 * Search/filter hook for the tree view.
 * Returns direct matches and the full set of nodes to show (including ancestors/descendants).
 */
export function useGraphSearch(graph: Graph, searchQuery: string): SearchResult {
  return useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    if (!query) {
      return { directMatches: null, filteredNodeSet: null };
    }

    const directMatches = new Set<string>();

    // Find all nodes that match the query
    Object.values(graph.nodes).forEach((node) => {
      const searchableText = [
        node.id,
        node.label,
        node.state,
        node.type,
        node.raw.kind || '',
        node.raw.runnerType || '',
        JSON.stringify(node.raw.options || {}),
        JSON.stringify(node.raw.args || {}),
      ].join(' ').toLowerCase();

      if (searchableText.includes(query)) {
        directMatches.add(node.id);
      }
    });

    if (directMatches.size === 0) {
      return { directMatches, filteredNodeSet: new Set() };
    }

    // Include all ancestors and descendants of matches
    const filteredNodeSet = new Set<string>(directMatches);

    // Add ancestors (so matches are visible in the tree)
    const ancestorStack = Array.from(directMatches);
    while (ancestorStack.length > 0) {
      const id = ancestorStack.pop()!;
      const node = graph.nodes[id];
      if (node) {
        node.parents.forEach((parentId) => {
          if (!filteredNodeSet.has(parentId)) {
            filteredNodeSet.add(parentId);
            ancestorStack.push(parentId);
          }
        });
      }
    }

    // Add descendants (to show context)
    const descendantStack = Array.from(directMatches);
    while (descendantStack.length > 0) {
      const id = descendantStack.pop()!;
      const node = graph.nodes[id];
      if (node) {
        node.children.forEach((childId) => {
          if (!filteredNodeSet.has(childId)) {
            filteredNodeSet.add(childId);
            descendantStack.push(childId);
          }
        });
      }
    }

    return { directMatches, filteredNodeSet };
  }, [graph, searchQuery]);
}
