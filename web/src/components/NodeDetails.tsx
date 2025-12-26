import { TimelineNode, AttemptInfo } from '../types';

interface NodeDetailsProps {
  node: TimelineNode;
  onClose: () => void;
}

export function NodeDetails({ node, onClose }: NodeDetailsProps) {
  const duration = calculateDuration(node.startTime, node.endTime);

  return (
    <div className="node-details">
      <div className="node-details-header">
        <h3>
          <span className={`node-type-badge ${node.type}`}>{node.type}</span>
          {' '}
          {node.id}
        </h3>
        <button className="node-details-close" onClick={onClose}>&times;</button>
      </div>
      <div className="node-details-content">
        <div className="node-details-section">
          <h4>Status</h4>
          <div className="node-details-field">
            <span className="node-details-field-label">State</span>
            <span className="node-details-field-value">
              <span className={`node-state-badge ${node.state}`}>{node.state}</span>
            </span>
          </div>
          {node.startTime && (
            <div className="node-details-field">
              <span className="node-details-field-label">Start Time</span>
              <span className="node-details-field-value">
                {formatTime(node.startTime)}
              </span>
            </div>
          )}
          {node.endTime && (
            <div className="node-details-field">
              <span className="node-details-field-label">End Time</span>
              <span className="node-details-field-value">
                {formatTime(node.endTime)}
              </span>
            </div>
          )}
          {duration && (
            <div className="node-details-field">
              <span className="node-details-field-label">Duration</span>
              <span className="node-details-field-value">{duration}</span>
            </div>
          )}
        </div>

        {node.type === 'check' && (
          <>
            {node.kind && (
              <div className="node-details-section">
                <h4>Kind</h4>
                <div className="node-details-field-value">{node.kind}</div>
              </div>
            )}
            {node.options && Object.keys(node.options).length > 0 && (
              <div className="node-details-section">
                <h4>Options</h4>
                <pre className="node-details-json">
                  {JSON.stringify(node.options, null, 2)}
                </pre>
              </div>
            )}
          </>
        )}

        {node.type === 'stage' && (
          <>
            {node.runnerType && (
              <div className="node-details-section">
                <h4>Runner</h4>
                <div className="node-details-field">
                  <span className="node-details-field-label">Type</span>
                  <span className="node-details-field-value">{node.runnerType}</span>
                </div>
                <div className="node-details-field">
                  <span className="node-details-field-label">Mode</span>
                  <span className="node-details-field-value">{node.executionMode || 'SYNC'}</span>
                </div>
              </div>
            )}
            {node.args && Object.keys(node.args).length > 0 && (
              <div className="node-details-section">
                <h4>Arguments</h4>
                <pre className="node-details-json">
                  {JSON.stringify(node.args, null, 2)}
                </pre>
              </div>
            )}
            {node.attempts && node.attempts.length > 0 && (
              <div className="node-details-section">
                <h4>Attempts</h4>
                <ul className="attempts-list">
                  {node.attempts.map((attempt) => (
                    <AttemptItem key={attempt.idx} attempt={attempt} />
                  ))}
                </ul>
              </div>
            )}
          </>
        )}

        {node.dependencies && node.dependencies.length > 0 && (
          <div className="node-details-section">
            <h4>Dependencies</h4>
            <ul style={{ listStyle: 'none', fontSize: '0.875rem' }}>
              {node.dependencies.map((dep) => (
                <li key={dep} style={{ fontFamily: 'ui-monospace, monospace' }}>
                  {dep}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </div>
  );
}

interface AttemptItemProps {
  attempt: AttemptInfo;
}

function AttemptItem({ attempt }: AttemptItemProps) {
  const duration = calculateDuration(attempt.startTime, attempt.endTime);

  return (
    <li className="attempt-item">
      <div className="attempt-header">
        <span className="attempt-number">Attempt #{attempt.idx}</span>
        <span className={`node-state-badge ${attempt.state}`}>{attempt.state}</span>
      </div>
      <div className="node-details-field">
        <span className="node-details-field-label">Start</span>
        <span className="node-details-field-value">{formatTime(attempt.startTime)}</span>
      </div>
      {attempt.endTime && (
        <div className="node-details-field">
          <span className="node-details-field-label">End</span>
          <span className="node-details-field-value">{formatTime(attempt.endTime)}</span>
        </div>
      )}
      {duration && (
        <div className="node-details-field">
          <span className="node-details-field-label">Duration</span>
          <span className="node-details-field-value">{duration}</span>
        </div>
      )}
      {attempt.failure && (
        <div style={{ marginTop: '0.5rem', color: '#991b1b' }}>
          Error: {attempt.failure.message}
        </div>
      )}
      {attempt.progress && attempt.progress.length > 0 && (
        <div style={{ marginTop: '0.5rem', fontSize: '0.75rem', color: '#6b7280' }}>
          Latest: {attempt.progress[attempt.progress.length - 1].message}
        </div>
      )}
    </li>
  );
}

function formatTime(timeStr: string): string {
  const date = new Date(timeStr);
  const timeString = date.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
  const ms = date.getMilliseconds().toString().padStart(3, '0');
  return `${timeString}.${ms}`;
}

function calculateDuration(startStr?: string, endStr?: string): string | null {
  if (!startStr) return null;

  const start = new Date(startStr).getTime();
  const end = endStr ? new Date(endStr).getTime() : Date.now();
  const ms = end - start;

  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`;
  if (ms < 3600000) return `${Math.floor(ms / 60000)}m ${((ms % 60000) / 1000).toFixed(0)}s`;
  return `${Math.floor(ms / 3600000)}h ${Math.floor((ms % 3600000) / 60000)}m`;
}
