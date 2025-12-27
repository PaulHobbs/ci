#!/bin/bash
# Script to run the submit queue with 8x8 matrix group testing

set -e

WORKSPACE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${WORKSPACE_ROOT}/bin"
RUNNER_DIR="${WORKSPACE_ROOT}/cmd/ci/runners/submit_queue"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}================================================${NC}"
echo -e "${BLUE}Submit Queue with 8x8 Matrix Group Testing${NC}"
echo -e "${BLUE}================================================${NC}"
echo ""

# Check if binaries exist, build if needed
if [ ! -f "${BIN_DIR}/submit_queue_runner" ]; then
    echo -e "${YELLOW}Building submit queue runner...${NC}"
    mkdir -p "${BIN_DIR}"
    go build -buildvcs=false -o "${BIN_DIR}/submit_queue_runner" "${RUNNER_DIR}"
fi

if [ ! -f "${BIN_DIR}/submit_queue" ]; then
    echo -e "${YELLOW}Building submit queue client...${NC}"
    go build -buildvcs=false -o "${BIN_DIR}/submit_queue" "${WORKSPACE_ROOT}/cmd/submit_queue"
fi

# Parse arguments
MODE="${1:-embedded}"
VERBOSE=""

if [ "$2" = "--verbose" ] || [ "$1" = "--verbose" ]; then
    VERBOSE="--verbose"
fi

if [ "$MODE" = "distributed" ]; then
    echo -e "${GREEN}Running in DISTRIBUTED mode${NC}"
    echo ""
    echo "You need to start the following runners manually:"
    echo ""
    echo "  Terminal 1 (Orchestrator):"
    echo "    ${BIN_DIR}/orchestrator --port 50051"
    echo ""
    echo "  Terminal 2 (Kickoff):"
    echo "    ${BIN_DIR}/submit_queue_runner --type kickoff --listen :50070"
    echo ""
    echo "  Terminal 3 (Config):"
    echo "    ${BIN_DIR}/submit_queue_runner --type config --listen :50071"
    echo ""
    echo "  Terminal 4 (Assignment):"
    echo "    ${BIN_DIR}/submit_queue_runner --type assignment --listen :50072"
    echo ""
    echo "  Terminal 5 (Planning):"
    echo "    ${BIN_DIR}/submit_queue_runner --type planning --listen :50073"
    echo ""
    echo "  Terminal 6 (Test Executor 1):"
    echo "    ${BIN_DIR}/submit_queue_runner --type test_executor --listen :50074 --runner-id test-1"
    echo ""
    echo "  Terminal 7 (Test Executor 2):"
    echo "    ${BIN_DIR}/submit_queue_runner --type test_executor --listen :50075 --runner-id test-2"
    echo ""
    echo "  Terminal 8 (Minibatch Finished):"
    echo "    ${BIN_DIR}/submit_queue_runner --type minibatch_finished --listen :50076"
    echo ""
    echo "  Terminal 9 (Batch Finished):"
    echo "    ${BIN_DIR}/submit_queue_runner --type batch_finished --listen :50077"
    echo ""
    echo "Then run the client:"
    echo "    ${BIN_DIR}/submit_queue --orchestrator localhost:50051 ${VERBOSE}"
    echo ""

elif [ "$MODE" = "embedded" ]; then
    echo -e "${GREEN}Running in EMBEDDED mode (orchestrator embedded in client)${NC}"
    echo ""
    echo "Starting submit queue with 64 CLs..."
    echo ""

    # Run the client with embedded orchestrator
    "${BIN_DIR}/submit_queue" --cls 64 ${VERBOSE}

else
    echo -e "Usage: $0 [embedded|distributed] [--verbose]"
    echo ""
    echo "  embedded (default) - Run with embedded orchestrator"
    echo "  distributed        - Show instructions for distributed mode"
    echo "  --verbose          - Enable verbose output"
    exit 1
fi
