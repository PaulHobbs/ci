import { useMemo } from 'react';
import { Graph, FlatTreeItem } from './buildGraph';

/**
 * Flattens a hierarchical graph into a list of items for rendering.
 * Tracks depth for indentation, marks repeated nodes, and applies filters.
 */
export function useFlattenedGraph(
  graph: Graph,
  expandedIds: Set<string>,
  filteredNodeSet: Set<string> | null,
  directMatches: Set<string> | null,
): FlatTreeItem[] {
  return useMemo(() => {
    const items: FlatTreeItem[] = [];
    const seenNodes = new Set<string>();
    const ancestorStack: string[] = [];

    const traverse = (nodeId: string, depth: number, parentId: string | null) => {
      // Skip if filtering is active and node is not in filtered set
      if (filteredNodeSet && !filteredNodeSet.has(nodeId)) {
        return;
      }

      const node = graph.nodes[nodeId];
      if (!node) return;

      const isRepeated = seenNodes.has(nodeId);
      const isCycle = ancestorStack.includes(nodeId);

      // Unique key for this appearance in the tree
      const key = parentId ? `${parentId}->${nodeId}` : nodeId;

      items.push({
        id: nodeId,
        key,
        depth,
        isRepeated,
        isCycle,
        hasChildren: node.children.size > 0,
        parentId,
        isMatch: directMatches ? directMatches.has(nodeId) : false,
      });

      seenNodes.add(nodeId);

      // Don't expand if:
      // - Node is collapsed
      // - Node is a cycle (would loop forever)
      // - Node is repeated AND collapsed (show summary instead)
      if (isCycle) return;
      if (!expandedIds.has(nodeId)) return;

      // Expand children
      ancestorStack.push(nodeId);
      const sortedChildren = Array.from(node.children).sort();
      sortedChildren.forEach((childId) => {
        traverse(childId, depth + 1, nodeId);
      });
      ancestorStack.pop();
    };

    // Start from roots
    const sortedRoots = [...graph.roots].sort();
    sortedRoots.forEach((rootId) => {
      traverse(rootId, 0, null);
    });

    return items;
  }, [graph, expandedIds, filteredNodeSet, directMatches]);
}
