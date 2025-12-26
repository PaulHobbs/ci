// Graph building utilities for the tree view

import { TimelineNode } from '../../types';

export type NodeType = 'check' | 'stage';

export interface GraphNode {
  id: string;
  type: NodeType;
  label: string;
  state: string;
  children: Set<string>;
  parents: Set<string>;
  raw: TimelineNode;
}

export interface Graph {
  nodes: Record<string, GraphNode>;
  roots: string[];
}

export interface FlatTreeItem {
  id: string;          // The graph node ID
  key: string;         // Unique rendering key for this specific row
  depth: number;
  isRepeated: boolean;
  isCycle: boolean;
  hasChildren: boolean;
  parentId: string | null;
  isMatch: boolean;    // True if this node directly matched the current filter
}

/**
 * Build a visual graph from timeline nodes.
 * Uses dependencies to establish parent-child relationships.
 */
export function buildVisualGraph(nodes: TimelineNode[]): Graph {
  const graphNodes: Record<string, GraphNode> = {};
  const allNodeIds = new Set<string>();
  const nonRootNodes = new Set<string>();

  // First pass: create all nodes
  nodes.forEach((node) => {
    const id = node.id;
    allNodeIds.add(id);
    graphNodes[id] = {
      id,
      type: node.type,
      label: id,
      state: node.state,
      children: new Set(),
      parents: new Set(),
      raw: node,
    };
  });

  // Second pass: establish parent-child relationships from dependencies
  // If A depends on B, then B is a parent of A (B -> A)
  nodes.forEach((node) => {
    if (node.dependencies && node.dependencies.length > 0) {
      node.dependencies.forEach((depId) => {
        if (graphNodes[depId] && graphNodes[node.id]) {
          // depId is a parent of node.id
          graphNodes[depId].children.add(node.id);
          graphNodes[node.id].parents.add(depId);
          nonRootNodes.add(node.id);
        }
      });
    }
  });

  // Roots are nodes with no parents
  const roots = Array.from(allNodeIds).filter((id) => !nonRootNodes.has(id));
  roots.sort();

  return { nodes: graphNodes, roots };
}

/**
 * Calculate the total number of unique nodes in a subtree.
 */
export function subtreeSize(graph: Graph, startNodeId: string): number {
  const visited = new Set<string>();
  const stack = [startNodeId];
  while (stack.length > 0) {
    const nodeId = stack.pop()!;
    if (!visited.has(nodeId)) {
      visited.add(nodeId);
      const node = graph.nodes[nodeId];
      if (node) {
        node.children.forEach((childId) => stack.push(childId));
      }
    }
  }
  return visited.size;
}

/**
 * Get all transitive descendants of given node(s).
 */
export function transitiveDescendants(
  graph: Graph,
  startIds: string | string[],
): Set<string> {
  const descendants = new Set<string>();
  const stack = Array.isArray(startIds) ? [...startIds] : [startIds];
  const visited = new Set<string>(stack);

  while (stack.length > 0) {
    const id = stack.pop()!;
    const node = graph.nodes[id];
    if (node) {
      node.children.forEach((childId) => {
        if (!visited.has(childId)) {
          visited.add(childId);
          descendants.add(childId);
          stack.push(childId);
        }
      });
    }
  }
  return descendants;
}
