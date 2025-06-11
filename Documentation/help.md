# GoParse Monitor - Verification Checklist

## ✅ Pre-Flight Checklist

Before running the monitor, verify:

### 1. Dependencies Installed
```bash
go mod tidy
```

### 2. Environment Variables Set
```bash
# Check Neo4j connection
echo $NEO4J_URI
echo $NEO4J_USER
echo $NEO4J_PASS

# For embeddings (optional)
echo $OPENAI_API_KEY
echo $OPENAI_BASE_URL
```

### 3. Build Successful
```bash
# Basic monitor
go build -o goparse-monitor cmd/monitor/main.go

# Enhanced monitor (if using)
go build -o goparse-monitor-enhanced cmd/monitor/enhanced_main.go
```

## 🧪 Verification Steps

### Step 1: Basic Functionality
```bash
# Run for 10 seconds and check output
timeout 10s ./goparse-monitor -root . || true
```

Expected: Should see "Monitoring . for changes. Press Ctrl+C to stop."

### Step 2: File Detection
```bash
# In terminal 1
./goparse-monitor -root /tmp/test-monitor

# In terminal 2
echo 'export function test() {}' > /tmp/test-monitor/test.ts
```

Expected: Monitor should log "Processing changed file: test.ts"

### Step 3: State Persistence
```bash
# After running monitor
ls -la /tmp/test-monitor/.goparse_state.json
```

Expected: State file should exist

### Step 4: API Server (Enhanced)
```bash
# Start with API
./goparse-monitor-enhanced -root . -api-port 8080 &

# Test API
curl http://localhost:8080/api/v1/status
```

Expected: JSON response with status

## 🚨 Common Issues & Solutions

### Issue: "command not found: go"
**Solution**: Install Go 1.21+ from https://golang.org/dl/

### Issue: "cannot find package"
**Solution**: Run `go mod download`

### Issue: "NEO4J_URI environment variable not set"
**Solution**: Export database credentials:
```bash
export NEO4J_URI="neo4j+s://..."
export NEO4J_USER="neo4j"
export NEO4J_PASS="password"
```

### Issue: "too many open files"
**Solution**:
```bash
# Linux
ulimit -n 10000

# Or permanently
echo fs.inotify.max_user_watches=524288 | sudo tee -a /etc/sysctl.conf
sudo sysctl -p
```

### Issue: Monitor not detecting changes
**Check**:
- File extension is supported (.ts, .tsx, .js, .jsx, .css, .scss)
- Directory isn't in skip list (node_modules, .git, etc.)
- File has actually changed (not just touched)

## 📊 Success Indicators

Your monitor is working correctly if you see:

1. ✅ "Monitoring X for changes" message
2. ✅ File changes logged when you modify files
3. ✅ State file created and updated
4. ✅ No error messages in output
5. ✅ Graceful shutdown on Ctrl+C

## 🎯 Next Steps

Once verified:

1. **Run on your codebase**: `./goparse-monitor -root /your/project`
2. **Enable features**: Add `-enable-batch`, `-enable-diff`, etc.
3. **Monitor performance**: Use `-api-port 8080` and check `/api/v1/stats`
4. **Deploy**: Use systemd/Docker for production

## 🆘 Need Help?

If verification fails:

1. Check the error message carefully
2. Verify all dependencies are installed
3. Ensure database is accessible
4. Check file permissions
5. Review the build output for errors

The monitor should now be working! 🎉