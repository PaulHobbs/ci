import { TimelineNode } from '../types';

interface GanttBarProps {
  node: TimelineNode;
  left: number;
  width: number;
  selected: boolean;
  onClick: () => void;
}

export function GanttBar({ node, left, width, selected, onClick }: GanttBarProps) {
  // Calculate duration for display
  const duration = (() => {
    if (!node.startTime) return '';
    const start = new Date(node.startTime).getTime();
    const end = node.endTime ? new Date(node.endTime).getTime() : Date.now();
    const ms = end - start;

    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
    return `${(ms / 60000).toFixed(1)}m`;
  })();

  return (
    <div
      className={`gantt-bar type-${node.type} state-${node.state} ${selected ? 'selected' : ''}`}
      style={{
        left: `${Math.max(0, left)}%`,
        width: `${Math.max(1, Math.min(100 - left, width))}%`,
      }}
      onClick={onClick}
      title={`${node.id} (${node.state}): ${duration}`}
    >
      {width > 5 && <span>{node.state}</span>}
    </div>
  );
}
