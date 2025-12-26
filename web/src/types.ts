// Types matching the Go API response structures

export interface TimelineResponse {
  workPlanId: string;
  createdAt: string;
  updatedAt: string;
  nodes: TimelineNode[];
}

export interface TimelineNode {
  id: string;
  type: 'check' | 'stage';
  state: string;
  startTime?: string;
  endTime?: string;
  dependencies?: string[];

  // Check-specific fields
  kind?: string;
  options?: Record<string, unknown>;

  // Stage-specific fields
  runnerType?: string;
  executionMode?: string;
  args?: Record<string, unknown>;
  attempts?: AttemptInfo[];
}

export interface AttemptInfo {
  idx: number;
  state: string;
  startTime: string;
  endTime?: string;
  progress?: ProgressInfo[];
  failure?: FailureInfo;
}

export interface ProgressInfo {
  message: string;
  timestamp: string;
  details?: Record<string, unknown>;
}

export interface FailureInfo {
  message: string;
  occurredAt: string;
}

export interface WorkPlanSummary {
  id: string;
  createdAt: string;
  updatedAt: string;
  metadata?: Record<string, string>;
  nodeCount: NodeCount;
}

export interface NodeCount {
  checks: number;
  stages: number;
}

export interface ListWorkPlansResponse {
  workPlans: WorkPlanSummary[];
}

// State colors for visualization
export const stateColors: Record<string, string> = {
  // Check states
  PLANNING: '#9ca3af',
  PLANNED: '#60a5fa',
  WAITING: '#fbbf24',
  FINAL: '#34d399',

  // Stage states
  ATTEMPTING: '#fbbf24',
  AWAITING_GROUP: '#a78bfa',

  // Attempt states
  PENDING: '#9ca3af',
  SCHEDULED: '#60a5fa',
  RUNNING: '#fbbf24',
  COMPLETE: '#34d399',
  INCOMPLETE: '#f87171',

  // Unknown
  UNKNOWN: '#6b7280',
};

export const typeColors: Record<string, string> = {
  check: '#3b82f6',
  stage: '#8b5cf6',
};
