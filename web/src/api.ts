import { useState, useEffect } from 'react';
import { ListWorkPlansResponse, TimelineResponse } from './types';

const API_BASE = '/api';

export async function listWorkPlans(): Promise<ListWorkPlansResponse> {
  const response = await fetch(`${API_BASE}/workplans`);
  if (!response.ok) {
    throw new Error(`Failed to list work plans: ${response.statusText}`);
  }
  return response.json();
}

export async function getTimeline(workPlanId: string): Promise<TimelineResponse> {
  const response = await fetch(`${API_BASE}/workplans/${workPlanId}/timeline`);
  if (!response.ok) {
    throw new Error(`Failed to get timeline: ${response.statusText}`);
  }
  return response.json();
}

// Hook for polling data
export function usePolling<T>(
  fetcher: () => Promise<T>,
  intervalMs: number = 2000
): { data: T | null; error: Error | null; loading: boolean } {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let mounted = true;

    const fetchData = async () => {
      try {
        const result = await fetcher();
        if (mounted) {
          setData(result);
          setError(null);
        }
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err : new Error(String(err)));
        }
      } finally {
        if (mounted) {
          setLoading(false);
        }
      }
    };

    fetchData();
    const interval = setInterval(fetchData, intervalMs);

    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, [fetcher, intervalMs]);

  return { data, error, loading };
}
