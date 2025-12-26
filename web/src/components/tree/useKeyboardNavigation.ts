import { useCallback, useEffect } from 'react';
import { Graph, FlatTreeItem, transitiveDescendants } from './buildGraph';

/**
 * Keyboard navigation hook for the tree view.
 * Supports vim-style j/k/h/l navigation plus expand/collapse shortcuts.
 */
export function useKeyboardNavigation(
  visibleItems: FlatTreeItem[],
  selectedKey: string | null,
  setSelectedKey: (key: string | null) => void,
  expandedIds: Set<string>,
  setExpandedIds: React.Dispatch<React.SetStateAction<Set<string>>>,
  graph: Graph,
) {
  // Auto-scroll selected item into view
  useEffect(() => {
    if (selectedKey) {
      const index = visibleItems.findIndex((i) => i.key === selectedKey);
      if (index >= 0) {
        const element = document.getElementById(`tree-row-${index}`);
        element?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      }
    }
  }, [selectedKey, visibleItems]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (visibleItems.length === 0) return;

      const currentIndex = visibleItems.findIndex((i) => i.key === selectedKey);
      const currentItem = currentIndex >= 0 ? visibleItems[currentIndex] : null;

      const selectIndex = (idx: number) => {
        if (idx >= 0 && idx < visibleItems.length) {
          setSelectedKey(visibleItems[idx].key);
        }
      };

      const findNextSibling = (direction: 1 | -1): number => {
        if (!currentItem) return -1;
        const siblings = visibleItems.filter(
          (i) => i.parentId === currentItem.parentId && i.depth === currentItem.depth
        );
        const siblingIdx = siblings.findIndex((s) => s.key === currentItem.key);
        const nextSibling = siblings[siblingIdx + direction];
        return nextSibling ? visibleItems.findIndex((i) => i.key === nextSibling.key) : -1;
      };

      switch (e.key) {
        case 'j':
        case 'ArrowDown':
          e.preventDefault();
          selectIndex(Math.min(currentIndex + 1, visibleItems.length - 1));
          break;

        case 'k':
        case 'ArrowUp':
          e.preventDefault();
          selectIndex(Math.max(currentIndex - 1, 0));
          break;

        case 'J': {
          e.preventDefault();
          const nextIdx = findNextSibling(1);
          if (nextIdx >= 0) selectIndex(nextIdx);
          break;
        }

        case 'K': {
          e.preventDefault();
          const prevIdx = findNextSibling(-1);
          if (prevIdx >= 0) selectIndex(prevIdx);
          break;
        }

        case 'h':
        case 'ArrowLeft':
          e.preventDefault();
          if (currentItem) {
            if (expandedIds.has(currentItem.id)) {
              // Collapse current node
              setExpandedIds((prev) => {
                const next = new Set(prev);
                next.delete(currentItem.id);
                return next;
              });
            } else if (currentItem.parentId) {
              // Move to parent
              const parentIdx = visibleItems.findIndex(
                (i) => i.id === currentItem.parentId && i.depth === currentItem.depth - 1
              );
              if (parentIdx >= 0) selectIndex(parentIdx);
            }
          }
          break;

        case 'l':
        case 'ArrowRight':
          e.preventDefault();
          if (currentItem && currentItem.hasChildren && !currentItem.isCycle) {
            if (!expandedIds.has(currentItem.id)) {
              // Expand current node
              setExpandedIds((prev) => new Set(prev).add(currentItem.id));
            } else {
              // Move to first child
              selectIndex(currentIndex + 1);
            }
          }
          break;

        case 'H':
          // Collapse parent (go up one level and collapse)
          e.preventDefault();
          if (currentItem && currentItem.parentId) {
            const parentItem = visibleItems.find(
              (i) => i.id === currentItem.parentId
            );
            if (parentItem) {
              setSelectedKey(parentItem.key);
              setExpandedIds((prev) => {
                const next = new Set(prev);
                next.delete(parentItem.id);
                return next;
              });
            }
          }
          break;

        case 'L':
          // Recursive expand
          e.preventDefault();
          if (currentItem) {
            const descendants = transitiveDescendants(graph, currentItem.id);
            setExpandedIds((prev) => {
              const next = new Set(prev);
              next.add(currentItem.id);
              descendants.forEach((d) => next.add(d));
              return next;
            });
          }
          break;

        case 'o':
        case 'Enter':
        case ' ':
          // Toggle expand/collapse
          e.preventDefault();
          if (currentItem && currentItem.hasChildren && !currentItem.isCycle) {
            setExpandedIds((prev) => {
              const next = new Set(prev);
              if (next.has(currentItem.id)) {
                next.delete(currentItem.id);
              } else {
                next.add(currentItem.id);
              }
              return next;
            });
          }
          break;

        case 'O':
          // Recursive toggle
          e.preventDefault();
          if (currentItem) {
            const descendants = transitiveDescendants(graph, currentItem.id);
            const allExpanded = expandedIds.has(currentItem.id) &&
              Array.from(descendants).every((d) => expandedIds.has(d));

            setExpandedIds((prev) => {
              const next = new Set(prev);
              if (allExpanded) {
                next.delete(currentItem.id);
                descendants.forEach((d) => next.delete(d));
              } else {
                next.add(currentItem.id);
                descendants.forEach((d) => next.add(d));
              }
              return next;
            });
          }
          break;

        case 'Home':
          e.preventDefault();
          selectIndex(0);
          break;

        case 'End':
          e.preventDefault();
          selectIndex(visibleItems.length - 1);
          break;
      }
    },
    [visibleItems, selectedKey, setSelectedKey, expandedIds, setExpandedIds, graph]
  );

  return { handleKeyDown };
}
