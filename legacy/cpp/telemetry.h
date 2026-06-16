#pragma once

// Thin facade over prometheus-cpp so the rest of cpp_core doesn't need to
// know about its types. Series names mirror orchestration/telemetry/metrics.py
// so a single Prometheus scrape config can pull from both planes.

#include <prometheus/counter.h>
#include <prometheus/gauge.h>
#include <prometheus/registry.h>

#include <map>
#include <memory>
#include <string>

namespace telemetry {

class Registry {
   public:
    static Registry& instance();

    prometheus::Counter& counter(const std::string& name, const std::string& help);
    prometheus::Counter& counter(const std::string& name, const std::string& help,
                                 const std::map<std::string, std::string>& labels);
    prometheus::Gauge& gauge(const std::string& name, const std::string& help);

    // Render the full /metrics body in Prometheus text exposition format.
    std::string render() const;

    prometheus::Registry& impl() { return *registry_; }

   private:
    Registry();
    std::shared_ptr<prometheus::Registry> registry_;
};

}  // namespace telemetry
