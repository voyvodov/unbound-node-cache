// Generally this is copy/pasted with minor modifications from
// https://github.com/letsencrypt/unbound_exporter
// Nothing fancy, just avoiding the need to run additional exporter

package metrics

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

var (
	unboundUpDesc = prometheus.NewDesc(
		prometheus.BuildFQName("unbound", "nodecache", "up"),
		"Whether scraping Unbound's metrics was successful.",
		nil, nil)

	unboundHistogram = prometheus.NewDesc(
		prometheus.BuildFQName("unbound", "nodecache", "response_time_seconds"),
		"Query response time in seconds.",
		nil, nil)

	unboundMetrics = []*unboundMetric{
		newUnboundMetric(
			"answer_rcodes_total",
			"Total number of answers to queries, from cache or from recursion, by response code.",
			prometheus.CounterValue,
			[]string{"rcode"},
			"^num\\.answer\\.rcode\\.(\\w+)$"),
		newUnboundMetric(
			"answers_bogus",
			"Total number of answers that were bogus.",
			prometheus.CounterValue,
			nil,
			"^num\\.answer\\.bogus$"),
		newUnboundMetric(
			"answers_secure_total",
			"Total number of answers that were secure.",
			prometheus.CounterValue,
			nil,
			"^num\\.answer\\.secure$"),
		newUnboundMetric(
			"cache_hits_total",
			"Total number of queries that were successfully answered using a cache lookup.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread(\\d+)\\.num\\.cachehits$"),
		newUnboundMetric(
			"cache_misses_total",
			"Total number of cache queries that needed recursive processing.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread(\\d+)\\.num\\.cachemiss$"),
		newUnboundMetric(
			"memory_caches_bytes",
			"Memory in bytes in use by caches.",
			prometheus.GaugeValue,
			[]string{"cache"},
			"^mem\\.cache\\.(\\w+)$"),
		newUnboundMetric(
			"memory_modules_bytes",
			"Memory in bytes in use by modules.",
			prometheus.GaugeValue,
			[]string{"module"},
			"^mem\\.mod\\.(\\w+)$"),
		newUnboundMetric(
			"memory_sbrk_bytes",
			"Memory in bytes allocated through sbrk.",
			prometheus.GaugeValue,
			nil,
			"^mem\\.total\\.sbrk$"),
		newUnboundMetric(
			"prefetches_total",
			"Total number of cache prefetches performed.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread(\\d+)\\.num\\.prefetch$"),
		newUnboundMetric(
			"queries_total",
			"Total number of queries received.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread(\\d+)\\.num\\.queries$"),
		newUnboundMetric(
			"expired_total",
			"Total number of expired entries served.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread(\\d+)\\.num\\.expired$"),
		newUnboundMetric(
			"query_classes_total",
			"Total number of queries with a given query class.",
			prometheus.CounterValue,
			[]string{"class"},
			"^num\\.query\\.class\\.([\\w]+)$"),
		newUnboundMetric(
			"query_flags_total",
			"Total number of queries that had a given flag set in the header.",
			prometheus.CounterValue,
			[]string{"flag"},
			"^num\\.query\\.flags\\.([\\w]+)$"),
		newUnboundMetric(
			"query_ipv6_total",
			"Total number of queries that were made using IPv6 towards the Unbound server.",
			prometheus.CounterValue,
			nil,
			"^num\\.query\\.ipv6$"),
		newUnboundMetric(
			"query_opcodes_total",
			"Total number of queries with a given query opcode.",
			prometheus.CounterValue,
			[]string{"opcode"},
			"^num\\.query\\.opcode\\.([\\w]+)$"),
		newUnboundMetric(
			"query_tcp_total",
			"Total number of queries that were made using TCP towards the Unbound server.",
			prometheus.CounterValue,
			nil,
			"^num\\.query\\.tcp$"),
		newUnboundMetric(
			"query_tls_total",
			"Total number of queries that were made using TCP TLS towards the Unbound server.",
			prometheus.CounterValue,
			nil,
			"^num\\.query\\.tls$"),
		newUnboundMetric(
			"query_types_total",
			"Total number of queries with a given query type.",
			prometheus.CounterValue,
			[]string{"type"},
			"^num\\.query\\.type\\.([\\w]+)$"),
		newUnboundMetric(
			"request_list_current_all",
			"Current size of the request list, including internally generated queries.",
			prometheus.GaugeValue,
			[]string{"thread"},
			"^thread([0-9]+)\\.requestlist\\.current\\.all$"),
		newUnboundMetric(
			"request_list_current_user",
			"Current size of the request list, only counting the requests from client queries.",
			prometheus.GaugeValue,
			[]string{"thread"},
			"^thread([0-9]+)\\.requestlist\\.current\\.user$"),
		newUnboundMetric(
			"request_list_exceeded_total",
			"Number of queries that were dropped because the request list was full.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread([0-9]+)\\.requestlist\\.exceeded$"),
		newUnboundMetric(
			"request_list_overwritten_total",
			"Total number of requests in the request list that were overwritten by newer entries.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread([0-9]+)\\.requestlist\\.overwritten$"),
		newUnboundMetric(
			"recursive_replies_total",
			"Total number of replies sent to queries that needed recursive processing.",
			prometheus.CounterValue,
			[]string{"thread"},
			"^thread(\\d+)\\.num\\.recursivereplies$"),
		newUnboundMetric(
			"rrset_bogus_total",
			"Total number of rrsets marked bogus by the validator.",
			prometheus.CounterValue,
			nil,
			"^num\\.rrset\\.bogus$"),
		newUnboundMetric(
			"time_elapsed_seconds",
			"Time since last statistics printout in seconds.",
			prometheus.CounterValue,
			nil,
			"^time\\.elapsed$"),
		newUnboundMetric(
			"time_now_seconds",
			"Current time in seconds since 1970.",
			prometheus.GaugeValue,
			nil,
			"^time\\.now$"),
		newUnboundMetric(
			"time_up_seconds_total",
			"Uptime since server boot in seconds.",
			prometheus.CounterValue,
			nil,
			"^time\\.up$"),
		newUnboundMetric(
			"unwanted_queries_total",
			"Total number of queries that were refused or dropped because they failed the access control settings.",
			prometheus.CounterValue,
			nil,
			"^unwanted\\.queries$"),
		newUnboundMetric(
			"unwanted_replies_total",
			"Total number of replies that were unwanted or unsolicited.",
			prometheus.CounterValue,
			nil,
			"^unwanted\\.replies$"),
		newUnboundMetric(
			"recursion_time_seconds_avg",
			"Average time it took to answer queries that needed recursive processing (does not include in-cache requests).",
			prometheus.GaugeValue,
			nil,
			"^total\\.recursion\\.time\\.avg$"),
		newUnboundMetric(
			"recursion_time_seconds_median",
			"The median of the time it took to answer queries that needed recursive processing.",
			prometheus.GaugeValue,
			nil,
			"^total\\.recursion\\.time\\.median$"),
		newUnboundMetric(
			"msg_cache_count",
			"The Number of Messages cached",
			prometheus.GaugeValue,
			nil,
			"^msg\\.cache\\.count$"),
		newUnboundMetric(
			"rrset_cache_count",
			"The Number of rrset cached",
			prometheus.GaugeValue,
			nil,
			"^rrset\\.cache\\.count$"),
	}
)

type unboundMetric struct {
	desc      *prometheus.Desc
	valueType prometheus.ValueType
	pattern   *regexp.Regexp
}

type UnboundExporter struct {
	path string
}

func newUnboundMetric(name string, description string, valueType prometheus.ValueType, labels []string, pattern string) *unboundMetric {
	return &unboundMetric{
		desc: prometheus.NewDesc(
			prometheus.BuildFQName("unbound", "nodecache", name),
			description,
			labels,
			nil),
		valueType: valueType,
		pattern:   regexp.MustCompile(pattern),
	}
}

func CollectFromReader(file io.Reader, ch chan<- prometheus.Metric) error {
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	histogramPattern := regexp.MustCompile(`^histogram\.\d+\.\d+\.to\.(\d+\.\d+)$`)

	histogramCount := uint64(0)
	histogramAvg := float64(0)
	histogramBuckets := make(map[float64]uint64)

	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "=")
		if len(fields) != 2 {
			return fmt.Errorf(
				"%q is not a valid key-value pair",
				scanner.Text())
		}

		for _, metric := range unboundMetrics {
			if matches := metric.pattern.FindStringSubmatch(fields[0]); matches != nil {
				value, err := strconv.ParseFloat(fields[1], 64)

				if err != nil {
					return err
				}
				ch <- prometheus.MustNewConstMetric(
					metric.desc,
					metric.valueType,
					value,
					matches[1:]...)

				break
			}
		}

		if matches := histogramPattern.FindStringSubmatch(fields[0]); matches != nil {
			end, err := strconv.ParseFloat(matches[1], 64)
			if err != nil {
				return err
			}
			value, err := strconv.ParseUint(fields[1], 10, 64)

			if err != nil {
				return err
			}
			histogramBuckets[end] = value
			histogramCount += value
		} else if fields[0] == "total.recursion.time.avg" {
			value, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				return err
			}
			histogramAvg = value
		}
	}

	// Convert the metrics to a cumulative Prometheus histogram.
	// Reconstruct the sum of all samples from the average value
	// provided by Unbound. Hopefully this does not break
	// monotonicity.
	keys := []float64{}
	for k := range histogramBuckets {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	prev := uint64(0)
	for _, i := range keys {
		histogramBuckets[i] += prev
		prev = histogramBuckets[i]
	}
	ch <- prometheus.MustNewConstHistogram(
		unboundHistogram,
		histogramCount,
		histogramAvg*float64(histogramCount),
		histogramBuckets)

	return scanner.Err()
}

func CollectFromSocket(path string, ch chan<- prometheus.Metric) error {
	var (
		conn net.Conn
		err  error
	)

	conn, err = net.Dial("unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte("UBCT1 stats_noreset\n"))
	if err != nil {
		return err
	}
	return CollectFromReader(conn, ch)
}

func NewUnboundExporter(path string) *UnboundExporter {
	return &UnboundExporter{
		path: path,
	}
}

func (e *UnboundExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- unboundUpDesc
	for _, metric := range unboundMetrics {
		ch <- metric.desc
	}
}

func (e *UnboundExporter) Collect(ch chan<- prometheus.Metric) {
	err := CollectFromSocket(e.path, ch)
	if err == nil {
		ch <- prometheus.MustNewConstMetric(
			unboundUpDesc,
			prometheus.GaugeValue,
			1.0)
	} else {
		klog.Errorf("Failed to scrape socket: ", err)
		ch <- prometheus.MustNewConstMetric(
			unboundUpDesc,
			prometheus.GaugeValue,
			0.0)
	}
}
