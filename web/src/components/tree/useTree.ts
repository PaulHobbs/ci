import { useCallback, useEffect, useRef, useState } from 'react';
import { Graph } from './buildGraph';
import { useFlattenedGraph } from './useFlattenedGraph';
import { useGraphSearch } from './useGraphSearch';
import { useKeyboardNavigation } from './useKeyboardNavigation';

export interface UseTreeProps {
  graph: Graph;
  defaultExpandedDepth?: number;
}

/**
 * Main state management hook for the tree view.
 * Handles expansion/collapse, keyboard navigation, and search/filtering.
 */
export function useTree({ graph, defaultExpandedDepth = 2 }: UseTreeProps) {
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  const initializedRef = useRef(false);

  // Initialize expanded state when graph changes
  useEffect(() => {
    if (graph.roots.length > 0) {
      const initialExpanded = new Set<string>();
      const stack = graph.roots.map((r) => ({ id: r, depth: 0 }));

      while (stack.length) {
        const { id, depth } = stack.pop()!;
        if (depth < defaultExpandedDepth) {
          initialExpanded.add(id);
          graph.nodes[id]?.children.forEach((c) =>
            stack.push({ id: c, depth: depth + 1 })
          );
        }
      }

      setExpandedIds(initialExpanded);
      initializedRef.current = true;
    }
  }, [graph, defaultExpandedDepth]);

  const { directMatches, filteredNodeSet } = useGraphSearch(graph, searchQuery);

  // Auto-expand relevant nodes when searching
  useEffect(() => {
    if (directMatches && directMatches.size > 0) {
      const toExpand = new Set<string>(directMatches);
      const stack = Array.from(directMatches);

      while (stack.length) {
        const id = stack.pop()!;
        graph.nodes[id]?.parents.forEach((p) => {
          if (!toExpand.has(p)) {
            toExpand.add(p);
            stack.push(p);
          }
        });
      }

      setExpandedIds(toExpand);
    }
  }, [graph, directMatches]);

  const visibleItems = useFlattenedGraph(
    graph,
    expandedIds,
    filteredNodeSet,
    directMatches
  );

  // Ensure selection is always valid
  useEffect(() => {
    if (
      visibleItems.length > 0 &&
      (!selectedKey || !visibleItems.some((i) => i.key === selectedKey))
    ) {
      setSelectedKey(visibleItems[0].key);
    }
  }, [visibleItems, selectedKey]);

  const { handleKeyDown } = useKeyboardNavigation(
    visibleItems,
    selectedKey,
    setSelectedKey,
    expandedIds,
    setExpandedIds,
    graph
  );

  const toggleKey = useCallback(
    (key: string) => {
      const item = visibleItems.find((i) => i.key === key);
      if (item) {
        setExpandedIds((prev) => {
          const next = new Set(prev);
          if (next.has(item.id)) {
            next.delete(item.id);
          } else {
            next.add(item.id);
          }
          return next;
        });
      }
    },
    [visibleItems]
  );

  return {
    visibleItems,
    selectedKey,
    setSelectedKey,
    expandedIds,
    toggleKey,
    handleKeyDown,
    searchQuery,
    setSearchQuery,
  };
}
