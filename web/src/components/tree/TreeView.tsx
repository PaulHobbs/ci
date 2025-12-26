import { memo, useCallback, useMemo, useRef, KeyboardEvent } from 'react';
import { usePolling, getTimeline } from '../../api';
import { TimelineNode, typeColors } from '../../types';
import { buildVisualGraph, FlatTreeItem, GraphNode, subtreeSize } from './buildGraph';
import { useTree } from './useTree';

// Status icons as text (simpler than importing icon library)
const StatusIcon = ({ node }: { node: GraphNode }) => {
  const state = node.state;
  switch (state) {
    case 'FINAL':
    case 'COMPLETE':
      return <span className="tree-icon tree-icon-success">✓</span>;
    case 'INCOMPLETE':
    case 'FAILED':
      return <span className="tree-icon tree-icon-failure">✗</span>;
    case 'RUNNING':
    case 'ATTEMPTING':
    case 'WAITING':
      return <span className="tree-icon tree-icon-running">●</span>;
    case 'PENDING':
    case 'PLANNED':
    case 'PLANNING':
      return <span className="tree-icon tree-icon-pending">○</span>;
    default:
      return <span className="tree-icon tree-icon-unknown">?</span>;
  }
};

// Single row in the tree
const TreeRow = memo(
  ({
    item,
    node,
    index,
    isSelected,
    isExpanded,
    onClick,
    onToggle,
    subtreeSizeForRepeat,
    treeContainerRef,
  }: {
    item: FlatTreeItem;
    node: GraphNode;
    index: number;
    isSelected: boolean;
    isExpanded: boolean;
    onClick: () => void;
    onToggle: () => void;
    subtreeSizeForRepeat?: number;
    treeContainerRef: React.RefObject<HTMLDivElement | null>;
  }) => {
    const handleClick = () => {
      onClick();
      treeContainerRef.current?.focus({ preventScroll: true });
    };

    const handleToggleClick = (e: React.MouseEvent) => {
      e.stopPropagation();
      onToggle();
      treeContainerRef.current?.focus({ preventScroll: true });
    };

    // Collapsed repeated node - show summary
    if (item.isRepeated && !isExpanded) {
      return (
        <div
          id={`tree-row-${index}`}
          className={`tree-row tree-row-repeated ${isSelected ? 'selected' : ''}`}
          style={{ paddingLeft: `${item.depth * 24 + 12}px` }}
          onClick={handleToggleClick}
          role="button"
          tabIndex={0}
        >
          <span className="tree-chevron tree-chevron-placeholder">⋯</span>
          <span className="tree-repeated-label">
            ... {subtreeSizeForRepeat} repeated {subtreeSizeForRepeat === 1 ? 'node' : 'nodes'}
          </span>
        </div>
      );
    }

    return (
      <div
        id={`tree-row-${index}`}
        className={`tree-row ${isSelected ? 'selected' : ''} ${item.isMatch ? 'matched' : ''} ${item.isRepeated ? 'repeated' : ''}`}
        style={{ paddingLeft: `${item.depth * 24 + 12}px` }}
        onClick={handleClick}
        role="treeitem"
        aria-expanded={item.hasChildren ? isExpanded : undefined}
        aria-level={item.depth + 1}
        aria-selected={isSelected}
        tabIndex={0}
      >
        <span
          className={`tree-chevron ${item.hasChildren && !item.isCycle ? 'clickable' : ''}`}
          onClick={item.hasChildren && !item.isCycle ? handleToggleClick : undefined}
        >
          {item.hasChildren && !item.isCycle ? (
            isExpanded ? '▼' : '▶'
          ) : (
            <span className="tree-chevron-placeholder">•</span>
          )}
        </span>
        <StatusIcon node={node} />
        <span className={`tree-label ${node.type === 'check' ? 'tree-label-check' : 'tree-label-stage'}`}>
          {node.label}
        </span>
        {node.type === 'stage' && (
          <span className="tree-type-badge">[Stage]</span>
        )}
        {item.isCycle && (
          <span className="tree-cycle-badge">(cycle)</span>
        )}
      </div>
    );
  }
);
TreeRow.displayName = 'TreeRow';

interface TreeViewProps {
  workPlanId: string;
  selectedNodeId?: string;
  onSelectNode: (node: TimelineNode) => void;
}

export function TreeView({ workPlanId, onSelectNode }: TreeViewProps) {
  const fetcher = useCallback(
    () => getTimeline(workPlanId),
    [workPlanId]
  );

  const { data, error, loading } = usePolling(fetcher, 2000);

  const graph = useMemo(() => {
    if (!data?.nodes) return { nodes: {}, roots: [] };
    return buildVisualGraph(data.nodes);
  }, [data]);

  const {
    visibleItems,
    selectedKey,
    setSelectedKey,
    expandedIds,
    toggleKey,
    handleKeyDown,
    searchQuery,
    setSearchQuery,
  } = useTree({ graph });

  const searchInputRef = useRef<HTMLInputElement>(null);
  const treeContainerRef = useRef<HTMLDivElement>(null);

  // Get the selected node for details panel
  const selectedItem = useMemo(() => {
    return visibleItems.find((item) => item.key === selectedKey);
  }, [visibleItems, selectedKey]);

  const selectedNode = useMemo(() => {
    return selectedItem ? graph.nodes[selectedItem.id] : undefined;
  }, [graph, selectedItem]);

  // Notify parent when selection changes
  useMemo(() => {
    if (selectedNode?.raw) {
      onSelectNode(selectedNode.raw);
    }
  }, [selectedNode, onSelectNode]);

  const onTreeKeyDown = (e: KeyboardEvent) => {
    // '/' to focus search
    if (e.key === '/') {
      e.preventDefault();
      searchInputRef.current?.focus();
      return;
    }
    // ESC to clear active search
    if (e.key === 'Escape' && searchQuery) {
      e.preventDefault();
      setSearchQuery('');
      return;
    }
    handleKeyDown(e);
  };

  const onSearchKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === 'Escape') {
      e.preventDefault();
      if (e.key === 'Escape') {
        setSearchQuery('');
      }
      treeContainerRef.current?.focus({ preventScroll: true });
    }
  };

  if (loading && !data) {
    return <div className="loading">Loading timeline...</div>;
  }

  if (error) {
    return <div className="error">Error loading timeline: {error.message}</div>;
  }

  return (
    <div className="tree-container">
      <div className="tree-header">
        <h2>Graph View</h2>
        <div className="gantt-legend">
          <div className="gantt-legend-item">
            <div className="gantt-legend-color" style={{ backgroundColor: typeColors.check }} />
            <span>Check</span>
          </div>
          <div className="gantt-legend-item">
            <div className="gantt-legend-color" style={{ backgroundColor: typeColors.stage }} />
            <span>Stage</span>
          </div>
        </div>
      </div>

      <div className="tree-search-bar">
        <div className="tree-search-wrapper">
          <span className="tree-search-icon">🔍</span>
          <input
            ref={searchInputRef}
            type="text"
            className={`tree-search-input ${searchQuery ? 'active' : ''}`}
            placeholder="Filter nodes... (/ to focus)"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={onSearchKeyDown}
          />
        </div>
        <div className="tree-keyboard-hints">
          <span><kbd>/</kbd> filter</span>
          <span><kbd>esc</kbd> clear</span>
          <span><kbd>j</kbd>/<kbd>k</kbd> move</span>
          <span><kbd>h</kbd>/<kbd>l</kbd> collapse/expand</span>
          <span><kbd>o</kbd> toggle</span>
        </div>
      </div>

      <div
        ref={treeContainerRef}
        className="tree-content"
        tabIndex={0}
        role="tree"
        onKeyDown={onTreeKeyDown}
      >
        {visibleItems.length === 0 ? (
          <div className="tree-empty">
            {searchQuery ? 'No nodes match your filter.' : 'No graph data available.'}
          </div>
        ) : (
          visibleItems.map((item, index) => {
            const node = graph.nodes[item.id];
            const isCollapsedRepeat = item.isRepeated && !expandedIds.has(item.id);
            const size = isCollapsedRepeat ? subtreeSize(graph, item.id) : undefined;

            return (
              <TreeRow
                key={item.key}
                index={index}
                item={item}
                node={node}
                isSelected={selectedKey === item.key}
                isExpanded={expandedIds.has(item.id)}
                subtreeSizeForRepeat={size}
                treeContainerRef={treeContainerRef}
                onClick={() => setSelectedKey(item.key)}
                onToggle={() => {
                  setSelectedKey(item.key);
                  toggleKey(item.key);
                }}
              />
            );
          })
        )}
      </div>
    </div>
  );
}
