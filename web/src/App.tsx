import { useState, useCallback } from 'react';
import { WorkPlanList } from './components/WorkPlanList';
import { GanttChart } from './components/GanttChart';
import { NodeDetails } from './components/NodeDetails';
import { TreeView } from './components/tree';
import { TimelineNode } from './types';

type ViewMode = 'gantt' | 'tree';

function App() {
  const [selectedWorkPlanId, setSelectedWorkPlanId] = useState<string | null>(null);
  const [selectedNode, setSelectedNode] = useState<TimelineNode | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>('gantt');

  const handleSelectWorkPlan = useCallback((id: string) => {
    setSelectedWorkPlanId(id);
    setSelectedNode(null);
  }, []);

  const handleSelectNode = useCallback((node: TimelineNode) => {
    setSelectedNode(node);
  }, []);

  const handleCloseDetails = useCallback(() => {
    setSelectedNode(null);
  }, []);

  return (
    <div className="app">
      <header className="header">
        <h1>TurboCI-Lite</h1>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div className="view-toggle">
            <button
              className={`view-toggle-btn ${viewMode === 'gantt' ? 'active' : ''}`}
              onClick={() => setViewMode('gantt')}
            >
              Timeline
            </button>
            <button
              className={`view-toggle-btn ${viewMode === 'tree' ? 'active' : ''}`}
              onClick={() => setViewMode('tree')}
            >
              Graph
            </button>
          </div>
          <div className="refresh-indicator">Auto-refresh: 2s</div>
        </div>
      </header>
      <main className="main">
        <aside className="sidebar">
          <h2 style={{ fontSize: '0.875rem', fontWeight: 600, marginBottom: '1rem', color: '#374151' }}>
            Work Plans
          </h2>
          <WorkPlanList
            selectedId={selectedWorkPlanId}
            onSelect={handleSelectWorkPlan}
          />
        </aside>
        <section className="content">
          {selectedWorkPlanId ? (
            <>
              {viewMode === 'gantt' ? (
                <GanttChart
                  workPlanId={selectedWorkPlanId}
                  selectedNodeId={selectedNode?.id}
                  onSelectNode={handleSelectNode}
                />
              ) : (
                <TreeView
                  workPlanId={selectedWorkPlanId}
                  selectedNodeId={selectedNode?.id}
                  onSelectNode={handleSelectNode}
                />
              )}
              {selectedNode && (
                <NodeDetails
                  node={selectedNode}
                  onClose={handleCloseDetails}
                />
              )}
            </>
          ) : (
            <div className="empty-state">
              <div className="empty-state-icon">📊</div>
              <p>Select a work plan to view its timeline</p>
            </div>
          )}
        </section>
      </main>
    </div>
  );
}

export default App;
