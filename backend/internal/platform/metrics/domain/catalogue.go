package domain

// Metric units used by the catalogue.
const (
	UnitPercent     = "percent"
	UnitBytes       = "bytes"
	UnitCount       = "count"
	UnitSeconds     = "seconds"
	UnitNanoseconds = "nanoseconds"
	UnitInfo        = "info"
)

// Catalogue metric names. Keep this list as the single source of truth for
// collectors, the future admin API, and the UI.
const (
	MetricProcessCPUPercent      = "process.cpu_percent"
	MetricProcessRSSBytes        = "process.rss_bytes"
	MetricProcessHeapBytes       = "process.heap_bytes"
	MetricProcessGCPauseNS       = "process.gc_pause_ns"
	MetricProcessGCCount         = "process.gc_count"
	MetricProcessGoroutines      = "process.goroutines"
	MetricProcessOpenFDs         = "process.open_fds"
	MetricProcessUptimeSeconds   = "process.uptime_seconds"
	MetricProcessBuildInfo       = "process.build_info"
	MetricProcessNetworkBytesIn  = "process.network_bytes_in"
	MetricProcessNetworkBytesOut = "process.network_bytes_out"

	MetricQueueJobsAvailable      = "queue.jobs_available"
	MetricQueueJobsRunning        = "queue.jobs_running"
	MetricQueueJobsRetryable      = "queue.jobs_retryable"
	MetricQueueJobsDiscarded      = "queue.jobs_discarded"
	MetricQueueJobsCompleted1m    = "queue.jobs_completed_1m"
	MetricQueueOldestAvailableAge = "queue.oldest_available_age_seconds"
	MetricQueueWorkerHeartbeat    = "queue.worker_heartbeat"
	MetricOutboxBacklog           = "outbox.backlog"
	MetricOutboxLagSeconds        = "outbox.lag_seconds"

	MetricGrowthTableRowsApprox = "growth.table_rows_approx"
	MetricGrowthDatabaseBytes   = "growth.database_bytes"
	MetricGrowthTableBytes      = "growth.table_bytes"
	MetricGrowthRedisUsedMemory = "growth.redis_used_memory_bytes"
	MetricGrowthRedisKeys       = "growth.redis_keys"
	MetricGrowthESDocs          = "growth.es_docs"
	MetricGrowthESStoreBytes    = "growth.es_store_bytes"
	MetricGrowthMediaUsedBytes  = "growth.media_used_bytes"
	MetricGrowthMediaFreeBytes  = "growth.media_free_bytes"

	MetricCollectorErrors = "metrics.collector_errors"
)

// Entry describes one catalogue metric.
type Entry struct {
	Name        string
	Unit        string
	Description string
}

// Catalogue returns every metric name, unit and description collectors may emit.
func Catalogue() []Entry {
	return []Entry{
		{Name: MetricProcessCPUPercent, Unit: UnitPercent, Description: "Process CPU utilisation from /proc/self/stat deltas"},
		{Name: MetricProcessRSSBytes, Unit: UnitBytes, Description: "Resident set size from /proc/self/statm"},
		{Name: MetricProcessHeapBytes, Unit: UnitBytes, Description: "Go heap in-use bytes from runtime/metrics"},
		{Name: MetricProcessGCPauseNS, Unit: UnitNanoseconds, Description: "Total GC pause nanoseconds from runtime.MemStats"},
		{Name: MetricProcessGCCount, Unit: UnitCount, Description: "Completed GC cycles from runtime/metrics"},
		{Name: MetricProcessGoroutines, Unit: UnitCount, Description: "Live goroutine count"},
		{Name: MetricProcessOpenFDs, Unit: UnitCount, Description: "Open file descriptors under /proc/self/fd"},
		{Name: MetricProcessUptimeSeconds, Unit: UnitSeconds, Description: "Seconds since the process started"},
		{Name: MetricProcessBuildInfo, Unit: UnitInfo, Description: "Build version marker (value 1, version label)"},
		{Name: MetricProcessNetworkBytesIn, Unit: UnitBytes, Description: "HTTP request/response bytes received by this process"},
		{Name: MetricProcessNetworkBytesOut, Unit: UnitBytes, Description: "HTTP request/response bytes sent by this process"},

		{Name: MetricQueueJobsAvailable, Unit: UnitCount, Description: "River jobs in available state per queue"},
		{Name: MetricQueueJobsRunning, Unit: UnitCount, Description: "River jobs in running state per queue"},
		{Name: MetricQueueJobsRetryable, Unit: UnitCount, Description: "River jobs in retryable state per queue"},
		{Name: MetricQueueJobsDiscarded, Unit: UnitCount, Description: "River jobs in discarded state per queue"},
		{Name: MetricQueueJobsCompleted1m, Unit: UnitCount, Description: "River jobs completed in the last minute per queue"},
		{Name: MetricQueueOldestAvailableAge, Unit: UnitSeconds, Description: "Age of the oldest available River job per queue"},
		{Name: MetricQueueWorkerHeartbeat, Unit: UnitInfo, Description: "Per-role worker heartbeat (value 1 when seen)"},
		{Name: MetricOutboxBacklog, Unit: UnitCount, Description: "Unprocessed outbox_events rows"},
		{Name: MetricOutboxLagSeconds, Unit: UnitSeconds, Description: "Age of the oldest unprocessed outbox event"},

		{Name: MetricGrowthTableRowsApprox, Unit: UnitCount, Description: "Approximate row count from pg_class.reltuples"},
		{Name: MetricGrowthDatabaseBytes, Unit: UnitBytes, Description: "PostgreSQL database size"},
		{Name: MetricGrowthTableBytes, Unit: UnitBytes, Description: "Top tables by total relation size"},
		{Name: MetricGrowthRedisUsedMemory, Unit: UnitBytes, Description: "Redis used_memory when configured"},
		{Name: MetricGrowthRedisKeys, Unit: UnitCount, Description: "Redis key count when configured"},
		{Name: MetricGrowthESDocs, Unit: UnitCount, Description: "Elasticsearch/OpenSearch document count when configured"},
		{Name: MetricGrowthESStoreBytes, Unit: UnitBytes, Description: "Elasticsearch/OpenSearch store size when configured"},
		{Name: MetricGrowthMediaUsedBytes, Unit: UnitBytes, Description: "Media blob directory used bytes"},
		{Name: MetricGrowthMediaFreeBytes, Unit: UnitBytes, Description: "Filesystem free space for the media directory"},

		{Name: MetricCollectorErrors, Unit: UnitCount, Description: "Collectors that failed or panicked in the last tick"},
	}
}

// KnownNames returns the set of catalogue metric names.
func KnownNames() map[string]struct{} {
	entries := Catalogue()
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[entry.Name] = struct{}{}
	}
	return out
}

// ValidateSamples reports the first sample whose name is not in the catalogue.
func ValidateSamples(samples []Sample) error {
	known := KnownNames()
	for _, sample := range samples {
		if _, ok := known[sample.Name]; !ok {
			return unknownMetricError(sample.Name)
		}
	}
	return nil
}

type unknownMetricError string

func (e unknownMetricError) Error() string {
	return "metrics: unknown metric " + string(e)
}
