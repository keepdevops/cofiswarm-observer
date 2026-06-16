#include "rss_generator.h"

#include <ctime>
#include <iomanip>
#include <sstream>

namespace rss_generator {
namespace {

struct State {
    bool enabled = false;
    std::size_t cap = 50;
    std::mutex mu;
    std::deque<Event> history, config, token_regulation;
};

State& st() { static State s; return s; }

std::deque<Event>& queue(Category c) {
    auto& s = st();
    if (c == Category::Config) return s.config;
    if (c == Category::TokenRegulation) return s.token_regulation;
    return s.history;
}

std::string xml_escape(const std::string& in) {
    std::string out;
    out.reserve(in.size());
    for (char ch : in) {
        if (ch == '&') out += "&amp;";
        else if (ch == '<') out += "&lt;";
        else if (ch == '>') out += "&gt;";
        else if (ch == '"') out += "&quot;";
        else if (ch == '\'') out += "&apos;";
        else out += ch;
    }
    return out;
}

std::string rfc822(std::chrono::system_clock::time_point tp) {
    std::time_t t = std::chrono::system_clock::to_time_t(tp);
    std::tm tm_buf{};
    gmtime_r(&t, &tm_buf);
    std::ostringstream os;
    os << std::put_time(&tm_buf, "%a, %d %b %Y %H:%M:%S +0000");
    return os.str();
}

}  // namespace

void configure(bool enabled, std::size_t max_per_category) {
    std::lock_guard<std::mutex> lk(st().mu);
    st().enabled = enabled;
    if (max_per_category > 0) st().cap = max_per_category;
}

bool is_enabled() {
    std::lock_guard<std::mutex> lk(st().mu);
    return st().enabled;
}

void publish(Category cat, const std::string& title,
             const std::string& description, const std::string& link) {
    std::lock_guard<std::mutex> lk(st().mu);
    if (!st().enabled) return;
    auto& q = queue(cat);
    q.push_front(Event{title, description, link, std::chrono::system_clock::now()});
    while (q.size() > st().cap) q.pop_back();
}

const char* category_path(Category cat) {
    if (cat == Category::Config) return "config";
    if (cat == Category::TokenRegulation) return "token-regulation";
    return "history";
}

std::string to_rss_xml(Category cat, const std::string& channel_link) {
    const char* path = category_path(cat);
    std::ostringstream xml;
    xml << "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<rss version=\"2.0\"><channel>\n"
        << "<title>Matrix " << path << "</title>\n<link>" << xml_escape(channel_link)
        << "</link>\n<description>Matrix coordinator " << path << " feed</description>\n";

    std::deque<Event> copy;
    {
        std::lock_guard<std::mutex> lk(st().mu);
        if (!st().enabled) return xml.str() + "</channel></rss>";
        copy = queue(cat);
    }
    for (const auto& ev : copy) {
        xml << "<item><title>" << xml_escape(ev.title) << "</title>\n<description>"
            << xml_escape(ev.description) << "</description>\n";
        if (!ev.link.empty()) xml << "<link>" << xml_escape(ev.link) << "</link>\n";
        xml << "<pubDate>" << rfc822(ev.at) << "</pubDate></item>\n";
    }
    return xml.str() + "</channel></rss>";
}

}  // namespace rss_generator
