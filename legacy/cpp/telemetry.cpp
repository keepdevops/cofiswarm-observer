#include "telemetry.h"

#include <prometheus/counter.h>
#include <prometheus/family.h>
#include <prometheus/gauge.h>
#include <prometheus/registry.h>
#include <prometheus/text_serializer.h>

#include <map>
#include <memory>
#include <mutex>
#include <sstream>
#include <unordered_map>

namespace telemetry {

namespace {

// Cache families so multiple lookups with the same name return the same
// Family<> instance. prometheus-cpp's BuildCounter/BuildGauge create a new
// family each time, which would register duplicate series.
std::mutex& family_mu() {
    static std::mutex m;
    return m;
}

std::unordered_map<std::string, prometheus::Family<prometheus::Counter>*>&
counter_families() {
    static std::unordered_map<std::string, prometheus::Family<prometheus::Counter>*> m;
    return m;
}

std::unordered_map<std::string, prometheus::Family<prometheus::Gauge>*>&
gauge_families() {
    static std::unordered_map<std::string, prometheus::Family<prometheus::Gauge>*> m;
    return m;
}

}  // namespace

Registry::Registry() : registry_(std::make_shared<prometheus::Registry>()) {}

Registry& Registry::instance() {
    static Registry r;
    return r;
}

prometheus::Counter& Registry::counter(const std::string& name, const std::string& help) {
    return counter(name, help, {});
}

prometheus::Counter& Registry::counter(
    const std::string& name, const std::string& help,
    const std::map<std::string, std::string>& labels) {
    prometheus::Family<prometheus::Counter>* fam = nullptr;
    {
        std::lock_guard<std::mutex> lock(family_mu());
        auto it = counter_families().find(name);
        if (it == counter_families().end()) {
            fam = &prometheus::BuildCounter().Name(name).Help(help).Register(*registry_);
            counter_families()[name] = fam;
        } else {
            fam = it->second;
        }
    }
    return fam->Add(labels);
}

prometheus::Gauge& Registry::gauge(const std::string& name, const std::string& help) {
    prometheus::Family<prometheus::Gauge>* fam = nullptr;
    {
        std::lock_guard<std::mutex> lock(family_mu());
        auto it = gauge_families().find(name);
        if (it == gauge_families().end()) {
            fam = &prometheus::BuildGauge().Name(name).Help(help).Register(*registry_);
            gauge_families()[name] = fam;
        } else {
            fam = it->second;
        }
    }
    return fam->Add({});
}

std::string Registry::render() const {
    std::ostringstream os;
    prometheus::TextSerializer ts;
    ts.Serialize(os, registry_->Collect());
    return os.str();
}

}  // namespace telemetry
