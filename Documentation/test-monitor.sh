#!/bin/bash
# test-monitor.sh - Test script for GoParse Monitor

set -e

echo "🧪 GoParse Monitor Test Script"
echo "=============================="

# Check if monitor binary exists
if [ ! -f "./goparse-monitor" ]; then
    echo "❌ Monitor binary not found. Building..."
    go build -o goparse-monitor cmd/monitor/main.go
    echo "✅ Monitor built successfully"
fi

# Create test directory
TEST_DIR="monitor-test-$$"
echo "📁 Creating test directory: $TEST_DIR"
mkdir -p "$TEST_DIR"

# Start monitor in background
echo "🚀 Starting monitor..."
./goparse-monitor -root "$TEST_DIR" &
MONITOR_PID=$!
echo "   Monitor PID: $MONITOR_PID"

# Give monitor time to start
sleep 2

# Function to create test file
create_file() {
    local filename=$1
    local content=$2
    echo "📝 Creating $filename"
    echo "$content" > "$TEST_DIR/$filename"
    sleep 1
}

# Function to modify file
modify_file() {
    local filename=$1
    local content=$2
    echo "✏️  Modifying $filename"
    echo "$content" >> "$TEST_DIR/$filename"
    sleep 1
}

# Function to delete file
delete_file() {
    local filename=$1
    echo "🗑️  Deleting $filename"
    rm -f "$TEST_DIR/$filename"
    sleep 1
}

# Run tests
echo ""
echo "🧪 Running tests..."
echo ""

# Test 1: Create TypeScript file
create_file "test1.ts" 'export function hello() { return "world"; }'

# Test 2: Create JavaScript file
create_file "test2.js" 'const greeting = () => "hello";'

# Test 3: Create CSS file
create_file "styles.css" '.button { color: blue; }'

# Test 4: Modify TypeScript file
modify_file "test1.ts" 'export function goodbye() { return "farewell"; }'

# Test 5: Create JSX file
create_file "component.jsx" 'export const Button = () => <button>Click me</button>;'

# Test 6: Delete file
delete_file "test2.js"

# Test 7: Create file in subdirectory
mkdir -p "$TEST_DIR/components"
create_file "components/Header.tsx" 'export const Header = () => <h1>Title</h1>;'

# Wait for final processing
sleep 2

# Stop monitor
echo ""
echo "🛑 Stopping monitor..."
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Check state file
if [ -f "$TEST_DIR/.goparse_state.json" ]; then
    echo "✅ State file created"
    echo "📊 State contents:"
    cat "$TEST_DIR/.goparse_state.json" | jq . 2>/dev/null || cat "$TEST_DIR/.goparse_state.json"
else
    echo "❌ State file not found"
fi

# Cleanup
echo ""
echo "🧹 Cleaning up..."
rm -rf "$TEST_DIR"

echo ""
echo "✅ Test completed!"
echo ""
echo "Check the monitor output above for:"
echo "  - File creation detection"
echo "  - File modification detection"
echo "  - File deletion detection"
echo "  - Subdirectory handling"