json = require "json"

done = function(summary, latency, requests)
    data = {}
    start = 0
    for i=1,summary.requests do
        table.insert(data, {
            Start = start,
            End = latency[i]/1000000,
            Percent = i/summary.requests*100,
            Count = 1
        })
        start = latency[i]/1000000
    end
    result = {
        RunType = "HTTP",
        Labels = "wrk2 Benchmark",
        DurationHistogram = {
            Count = summary.requests,
            Min = latency.min/1000000,
            Max = latency.max/1000000,
            Avg = latency.mean/1000000,
            StdDev = latency.stdev/1000000,
            Data = data,
            Percentiles = {
                {
                    Percentile = 50,
                    Value = latency:percentile(50)/1000000
                },
                {
                    Percentile = 75,
                    Value = latency:percentile(75)/1000000
                },
                {
                    Percentile = 99,
                    Value = latency:percentile(99)/1000000
                },
                {
                    Percentile = 99.9,
                    Value = latency:percentile(99.9)/1000000
                }
            }
        },
        RetCodes = {
            -- "200" = summary.requests
        },
        Sizes = {
            -- Count = summary.requests,
            -- Min = latency.min/1000000,
            -- Max = latency.max/1000000,
            -- Avg = latency.mean/1000000,
            -- StdDev = latency.stdev/1000000,
            -- Data = { --sizes maybe not needed?
            --     {
            --         Start = 0,
            --         End = latency.max/1000000,
            --         Percent = 100,
            --         Count = summary.requests
            --     }
            -- }
        },
        URL = "Unknown URL"
    }
    result["RetCodes"]["200"] = summary.requests - summary.errors.status
    io.write(json.encode(result))
end