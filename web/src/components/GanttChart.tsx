import { useCallback, useMemo } from 'react';
import { getTimeline, usePolling } from '../api';
import { TimelineNode } from '../types';
import { GanttBar } from './GanttBar';

interface GanttChartProps {
  workPlanId: string;
  selectedNodeId?: string;
  onSelectNode: (node: TimelineNode) => void;
}

export function GanttChart({ workPlanId, selectedNodeId, onSelectNode }: GanttChartProps) {
  const fetcher = useCallback(() => getTimeline(workPlanId), [workPlanId]);
  const { data, error, loading } = usePolling(fetcher, 2000);

  // Calculate time range for the chart
  const timeRange = useMemo(() => {
    if (!data?.nodes?.length) return null;

    let minTime = Infinity;
    let maxTime = -Infinity;

    for (const node of data.nodes) {
      if (node.startTime) {
        const start = new Date(node.startTime).getTime();
        minTime = Math.min(minTime, start);
      }
      if (node.endTime) {
        const end = new Date(node.endTime).getTime();
        maxTime = Math.max(maxTime, end);
      } else if (node.startTime) {
        // For running nodes, use current time as end
        maxTime = Math.max(maxTime, Date.now());
      }
    }

    if (minTime === Infinity || maxTime === -Infinity) return null;

    // Add 5% padding
    const duration = maxTime - minTime;
    const padding = Math.max(duration * 0.05, 1000); // At least 1 second padding

    return {
      start: minTime - padding,
      end: maxTime + padding,
      duration: maxTime - minTime + padding * 2,
    };
  }, [data]);

  // Generate time axis ticks
  const timeTicks = useMemo(() => {
    if (!timeRange) return [];

    const ticks: { time: number; label: string }[] = [];
    const tickCount = 6;
    const tickInterval = timeRange.duration / (tickCount - 1);

    for (let i = 0; i < tickCount; i++) {
      const time = timeRange.start + i * tickInterval;
      const date = new Date(time);
      ticks.push({
        time,
        label: date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
      });
    }

    return ticks;
  }, [timeRange]);

  // Sort nodes: checks first, then stages, both alphabetically
  const sortedNodes = useMemo(() => {
    if (!data?.nodes) return [];

    return [...data.nodes].sort((a, b) => {
      if (a.type !== b.type) {
        return a.type === 'check' ? -1 : 1;
      }
      return a.id.localeCompare(b.id);
    });
  }, [data]);

  if (loading && !data) {
    return <div className="gantt-container"><div className="loading">Loading timeline</div></div>;
  }

  if (error) {
    return <div className="gantt-container"><div className="error">Error: {error.message}</div></div>;
  }

  if (!data?.nodes?.length) {
    return (
      <div className="gantt-container">
        <div className="gantt-header">
          <h2>Timeline</h2>
        </div>
        <div className="empty-state">
          <p>No nodes in this work plan</p>
        </div>
      </div>
    );
  }

  return (
    <div className="gantt-container">
      <div className="gantt-header">
        <h2>Timeline</h2>
        <div className="gantt-legend">
          <div className="gantt-legend-item">
            <div className="gantt-legend-color" style={{ backgroundColor: '#3b82f6' }} />
            <span>Check</span>
          </div>
          <div className="gantt-legend-item">
            <div className="gantt-legend-color" style={{ backgroundColor: '#8b5cf6' }} />
            <span>Stage</span>
          </div>
        </div>
      </div>
      <div className="gantt-timeline">
        <div className="gantt-time-axis">
          {timeTicks.map((tick, i) => (
            <div key={i} className="gantt-time-tick">{tick.label}</div>
          ))}
        </div>
        <div className="gantt-rows">
          {sortedNodes.map((node) => (
            <GanttRow
              key={node.id}
              node={node}
              timeRange={timeRange!}
              selected={node.id === selectedNodeId}
              onClick={() => onSelectNode(node)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

interface GanttRowProps {
  node: TimelineNode;
  timeRange: { start: number; end: number; duration: number };
  selected: boolean;
  onClick: () => void;
}

function GanttRow({ node, timeRange, selected, onClick }: GanttRowProps) {
  const startTime = node.startTime ? new Date(node.startTime).getTime() : null;
  const endTime = node.endTime ? new Date(node.endTime).getTime() : (startTime ? Date.now() : null);

  if (!startTime || !endTime) {
    return (
      <div className="gantt-row">
        <div className="gantt-row-label" title={node.id}>
          {node.id}
        </div>
        <div className="gantt-row-track">
          {/* No bar for nodes without timing */}
        </div>
      </div>
    );
  }

  const left = ((startTime - timeRange.start) / timeRange.duration) * 100;
  const width = ((endTime - startTime) / timeRange.duration) * 100;

  return (
    <div className="gantt-row">
      <div className="gantt-row-label" title={node.id}>
        {node.id}
      </div>
      <div className="gantt-row-track">
        <GanttBar
          node={node}
          left={left}
          width={width}
          selected={selected}
          onClick={onClick}
        />
      </div>
    </div>
  );
}
