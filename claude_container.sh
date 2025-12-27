#!/bin/bash
set -e

# Configuration
WORKSPACE_NAME="${1:-claude-$(date +%s)}"
REPO_ROOT="$(git rev-parse --show-toplevel)"
WORKTREE_DIR="${REPO_ROOT}/../worktrees/${WORKSPACE_NAME}"
MAIN_BRANCH="$(git symbolic-ref --short HEAD 2>/dev/null || echo 'main')"
MERGE_SCRIPT="./merge_${WORKSPACE_NAME}.sh"
DISCARD_SCRIPT="./discard_${WORKSPACE_NAME}.sh"

echo "=== Claude Container Setup ==="
echo "Workspace: ${WORKSPACE_NAME}"
echo "Worktree:  ${WORKTREE_DIR}"
echo "Main branch: ${MAIN_BRANCH}"

# Create worktree directory parent if needed
mkdir -p "$(dirname "${WORKTREE_DIR}")"

# Create a new branch and worktree
BRANCH_NAME="claude/${WORKSPACE_NAME}"
echo ""
echo "Creating worktree with branch: ${BRANCH_NAME}"
git worktree add -b "${BRANCH_NAME}" "${WORKTREE_DIR}" HEAD

# Add CLAUDE.md instructions to always commit work
CLAUDE_MD="${WORKTREE_DIR}/CLAUDE.md"
COMMIT_INSTRUCTIONS="
## Container Workflow Instructions

**IMPORTANT**: You are running in an isolated worktree container.

Before ending your session, you MUST:
1. Stage all changes: \`git add -A\`
2. Commit with a descriptive message: \`git commit -m \"Description of changes\"\`
3. Confirm the commit was successful

The user will use the merge script to apply your changes to the main branch.
"

if [[ -f "${CLAUDE_MD}" ]]; then
    echo "${COMMIT_INSTRUCTIONS}" >> "${CLAUDE_MD}"
else
    echo "${COMMIT_INSTRUCTIONS}" > "${CLAUDE_MD}"
fi

echo "Updated CLAUDE.md with commit instructions"

# Create the merge script in the current directory
cat > "${MERGE_SCRIPT}" << 'MERGE_EOF'
#!/bin/bash
set -e

WORKSPACE_NAME="__WORKSPACE_NAME__"
WORKTREE_DIR="__WORKTREE_DIR__"
BRANCH_NAME="__BRANCH_NAME__"
MAIN_BRANCH="__MAIN_BRANCH__"
DISCARD_SCRIPT="__DISCARD_SCRIPT__"
SCRIPT_PATH="$0"

echo "=== Merging ${BRANCH_NAME} into ${MAIN_BRANCH} ==="

# Ensure we're in the main repo
cd "$(git rev-parse --show-toplevel)"

# Check for uncommitted changes in the worktree
if git -C "${WORKTREE_DIR}" status --porcelain | grep -q .; then
    echo "ERROR: Worktree has uncommitted changes!"
    echo "Please commit or stash changes in the worktree first."
    exit 1
fi

# Get the current branch
CURRENT_BRANCH="$(git symbolic-ref --short HEAD 2>/dev/null)"

# Switch to main branch if not already there
if [[ "${CURRENT_BRANCH}" != "${MAIN_BRANCH}" ]]; then
    echo "Switching to ${MAIN_BRANCH}..."
    git checkout "${MAIN_BRANCH}"
fi

# Merge the worktree branch
echo "Merging ${BRANCH_NAME}..."
if git merge "${BRANCH_NAME}" --no-edit; then
    echo ""
    echo "=== Merge successful! ==="

    # Clean up worktree
    echo "Removing worktree..."
    git worktree remove "${WORKTREE_DIR}" --force

    # Delete the branch
    echo "Deleting branch ${BRANCH_NAME}..."
    git branch -d "${BRANCH_NAME}"

    echo ""
    echo "=== Cleanup complete ==="

    # Remove discard script if it exists
    if [[ -f "${DISCARD_SCRIPT}" ]]; then
        echo "Removing discard script..."
        rm -f "${DISCARD_SCRIPT}"
    fi

    # Self-delete
    echo "Removing merge script..."
    rm -f "${SCRIPT_PATH}"

    echo "Done! Changes have been merged into ${MAIN_BRANCH}."
else
    echo ""
    echo "ERROR: Merge failed. Please resolve conflicts manually."
    echo "After resolving, you can run this script again or manually clean up:"
    echo "  git worktree remove ${WORKTREE_DIR}"
    echo "  git branch -d ${BRANCH_NAME}"
    exit 1
fi
MERGE_EOF

# Replace placeholders in merge script
sed -i '' "s|__WORKSPACE_NAME__|${WORKSPACE_NAME}|g" "${MERGE_SCRIPT}"
sed -i '' "s|__WORKTREE_DIR__|${WORKTREE_DIR}|g" "${MERGE_SCRIPT}"
sed -i '' "s|__BRANCH_NAME__|${BRANCH_NAME}|g" "${MERGE_SCRIPT}"
sed -i '' "s|__MAIN_BRANCH__|${MAIN_BRANCH}|g" "${MERGE_SCRIPT}"
sed -i '' "s|__DISCARD_SCRIPT__|${DISCARD_SCRIPT}|g" "${MERGE_SCRIPT}"

chmod +x "${MERGE_SCRIPT}"
echo "Created merge script: ${MERGE_SCRIPT}"

# Create the discard script in the current directory
cat > "${DISCARD_SCRIPT}" << 'DISCARD_EOF'
#!/bin/bash
set -e

WORKSPACE_NAME="__WORKSPACE_NAME__"
WORKTREE_DIR="__WORKTREE_DIR__"
BRANCH_NAME="__BRANCH_NAME__"
MERGE_SCRIPT="__MERGE_SCRIPT__"
SCRIPT_PATH="$0"

echo "=== Discarding workspace ${WORKSPACE_NAME} ==="
echo "WARNING: All uncommitted changes will be lost!"
echo ""
read -p "Are you sure? (yes/no): " -r CONFIRM

if [[ "${CONFIRM}" != "yes" ]]; then
    echo "Cancelled."
    exit 0
fi

echo ""

# Ensure we're in the main repo
cd "$(git rev-parse --show-toplevel)"

# Remove worktree
if [[ -d "${WORKTREE_DIR}" ]]; then
    echo "Removing worktree..."
    git worktree remove "${WORKTREE_DIR}" --force
else
    echo "Worktree already removed"
fi

# Delete the branch
if git rev-parse --verify "${BRANCH_NAME}" >/dev/null 2>&1; then
    echo "Deleting branch ${BRANCH_NAME}..."
    git branch -D "${BRANCH_NAME}"
else
    echo "Branch already deleted"
fi

# Remove merge script if it exists
if [[ -f "${MERGE_SCRIPT}" ]]; then
    echo "Removing merge script..."
    rm -f "${MERGE_SCRIPT}"
fi

echo ""
echo "=== Cleanup complete ==="

# Self-delete
echo "Removing discard script..."
rm -f "${SCRIPT_PATH}"

echo "Done! Workspace has been discarded."
DISCARD_EOF

# Replace placeholders in discard script
sed -i '' "s|__WORKSPACE_NAME__|${WORKSPACE_NAME}|g" "${DISCARD_SCRIPT}"
sed -i '' "s|__WORKTREE_DIR__|${WORKTREE_DIR}|g" "${DISCARD_SCRIPT}"
sed -i '' "s|__BRANCH_NAME__|${BRANCH_NAME}|g" "${DISCARD_SCRIPT}"
sed -i '' "s|__MERGE_SCRIPT__|${MERGE_SCRIPT}|g" "${DISCARD_SCRIPT}"

chmod +x "${DISCARD_SCRIPT}"
echo "Created discard script: ${DISCARD_SCRIPT}"

# Build Docker image from local .devcontainer if needed
DOCKER_IMAGE="claude-code-local:latest"
DEVCONTAINER_DIR="${REPO_ROOT}/deps/claude-code/.devcontainer"

echo ""
echo "=== Preparing Docker Image ==="

if [[ ! -d "${DEVCONTAINER_DIR}" ]]; then
    echo "ERROR: .devcontainer directory not found at ${DEVCONTAINER_DIR}"
    echo "Please ensure the claude-code submodule is initialized:"
    echo "  git submodule update --init --recursive"
    exit 1
fi

# Check if image exists
if ! docker image inspect "${DOCKER_IMAGE}" >/dev/null 2>&1; then
    echo "Building Docker image from .devcontainer..."
    docker build -t "${DOCKER_IMAGE}" "${DEVCONTAINER_DIR}"
else
    echo "Using existing Docker image: ${DOCKER_IMAGE}"
    echo "(To rebuild, run: docker rmi ${DOCKER_IMAGE})"
fi

# Run Claude in container with the worktree mounted
echo ""
echo "=== Starting Claude Container ==="
echo "Worktree mounted at: /workspace"
echo ""

docker run -it --rm \
    --cap-add=NET_ADMIN \
    --cap-add=NET_RAW \
    -v "${WORKTREE_DIR}:/workspace" \
    -v "${HOME}/.claude:/home/node/.claude" \
    -w /workspace \
    -e ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY}" \
    "${DOCKER_IMAGE}" \
    zsh -c "claude --dangerously-skip-permissions"

echo ""
echo "=== Container session ended ==="
echo ""
echo "Options:"
echo ""
echo "  Merge changes back to ${MAIN_BRANCH}:"
echo "    ${MERGE_SCRIPT}"
echo ""
echo "  Discard all changes:"
echo "    ${DISCARD_SCRIPT}"
