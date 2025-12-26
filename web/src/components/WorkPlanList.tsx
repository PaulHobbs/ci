import { useCallback } from 'react';
import { listWorkPlans, usePolling } from '../api';
import { WorkPlanSummary } from '../types';

interface WorkPlanListProps {
  selectedId: string | null;
  onSelect: (id: string) => void;
}

export function WorkPlanList({ selectedId, onSelect }: WorkPlanListProps) {
  const fetcher = useCallback(() => listWorkPlans(), []);
  const { data, error, loading } = usePolling(fetcher, 5000);

  if (loading && !data) {
    return <div className="loading">Loading work plans</div>;
  }

  if (error) {
    return <div className="error">Error: {error.message}</div>;
  }

  const workPlans = data?.workPlans || [];

  if (workPlans.length === 0) {
    return (
      <div className="empty-state" style={{ height: 'auto', padding: '2rem 0' }}>
        <p>No work plans found</p>
      </div>
    );
  }

  return (
    <ul className="workplan-list">
      {workPlans.map((wp) => (
        <WorkPlanItem
          key={wp.id}
          workPlan={wp}
          selected={wp.id === selectedId}
          onSelect={onSelect}
        />
      ))}
    </ul>
  );
}

interface WorkPlanItemProps {
  workPlan: WorkPlanSummary;
  selected: boolean;
  onSelect: (id: string) => void;
}

function WorkPlanItem({ workPlan, selected, onSelect }: WorkPlanItemProps) {
  const handleClick = () => onSelect(workPlan.id);

  const createdAt = new Date(workPlan.createdAt);
  const timeAgo = formatTimeAgo(createdAt);

  return (
    <li
      className={`workplan-item ${selected ? 'selected' : ''}`}
      onClick={handleClick}
    >
      <div className="workplan-item-id">
        {workPlan.id.slice(0, 8)}...
      </div>
      <div className="workplan-item-meta">
        {timeAgo}
      </div>
      <div className="workplan-item-counts">
        <span className="workplan-item-count">
          {workPlan.nodeCount.checks} checks
        </span>
        <span className="workplan-item-count">
          {workPlan.nodeCount.stages} stages
        </span>
      </div>
    </li>
  );
}

function formatTimeAgo(date: Date): string {
  const now = new Date();
  const diff = now.getTime() - date.getTime();

  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (days > 0) return `${days}d ago`;
  if (hours > 0) return `${hours}h ago`;
  if (minutes > 0) return `${minutes}m ago`;
  return 'just now';
}
