#include "agent_metrics.h"

#include <map>
#include <mutex>

using json = nlohmann::json;

namespace agent_metrics {

namespace {

struct Entry {
    int    calls = 0;
    double total_ms = 0.0;
    long   completion_tokens = 0;
    long   prompt_tokens = 0;   // total prompt size (cached + evaluated)
    long   cached_tokens = 0;   // prompt tokens reused from the KV prompt-cache
    bool   has_cache = false;   // true once any call reported a cache figure
};

std::map<std::string, Entry> g_state;
std::mutex g_mu;

} // namespace

void reset() {
    std::lock_guard<std::mutex> lk(g_mu);
    g_state.clear();
}

void record(const std::string& name, double duration_ms,
            long completion_tokens, long prompt_tokens, long cached_tokens) {
    if (name.empty()) return;
    std::lock_guard<std::mutex> lk(g_mu);
    Entry& e = g_state[name];
    ++e.calls;
    e.total_ms += duration_ms;
    if (completion_tokens > 0) e.completion_tokens += completion_tokens;
    if (prompt_tokens > 0)     e.prompt_tokens     += prompt_tokens;
    if (cached_tokens >= 0)  { e.cached_tokens += cached_tokens; e.has_cache = true; }
}

json snapshot() {
    std::lock_guard<std::mutex> lk(g_mu);
    json out = json::object();
    for (auto& kv : g_state) {
        const Entry& e = kv.second;
        json entry = {
            {"calls", e.calls},
            {"total_ms", e.total_ms},
            {"completion_tokens", e.completion_tokens},
        };
        if (e.calls > 1) entry["avg_ms"] = e.total_ms / e.calls;
        if (e.prompt_tokens > 0) {
            entry["prompt_tokens"] = e.prompt_tokens;
            // KV prompt-cache reuse: cached prefix tokens / total prompt tokens.
            if (e.has_cache) {
                entry["prompt_cached"] = e.cached_tokens;
                entry["prompt_evaluated"] = e.prompt_tokens - e.cached_tokens;
                entry["kv_reuse"] =
                    static_cast<double>(e.cached_tokens) / static_cast<double>(e.prompt_tokens);
            }
        }
        out[kv.first] = entry;
    }
    return out;
}

} // namespace agent_metrics
