#pragma once
// MS-69: opt-in RSS 2.0 event feed (zero cost when disabled).

#include <chrono>
#include <cstddef>
#include <deque>
#include <mutex>
#include <string>

namespace rss_generator {

enum class Category { History, Config, TokenRegulation };

struct Event {
    std::string title;
    std::string description;
    std::string link;
    std::chrono::system_clock::time_point at{};
};

void configure(bool enabled, std::size_t max_per_category = 50);
bool is_enabled();

void publish(Category cat, const std::string& title,
             const std::string& description, const std::string& link = "");

/// RSS 2.0 XML for a category feed (empty channel when disabled).
std::string to_rss_xml(Category cat, const std::string& channel_link);

const char* category_path(Category cat);

}  // namespace rss_generator
