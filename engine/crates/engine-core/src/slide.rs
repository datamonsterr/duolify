//! Conservative static checks; a sandbox and restrictive CSP are still required at playback.
use regex::Regex;
pub const MAX_BYTES: usize = 256 * 1024;
pub fn validate(html: &str, allow_hosts: &[&str]) -> Result<(), &'static str> {
    if html.len() > MAX_BYTES {
        return Err("slide exceeds size limit");
    }
    let stripped = Regex::new(r"(?s)<!--.*?-->").unwrap().replace_all(html, "");
    let root =
        Regex::new(r#"(?i)<[a-z][a-z0-9-]*\b[^>]*\bid\s*=\s*(?:"root"|'root'|root(?:\s|>))"#)
            .unwrap();
    if !root.is_match(&stripped) {
        return Err("slide must contain a root element");
    }
    // Reject obfuscation and dynamic network primitives rather than trying to interpret JS.
    let dangerous = Regex::new(r"(?i)(?:&#|\\|\b(?:fetch|websocket|eventsource|xmlhttprequest|sendbeacon|importscripts)\b|@import|<\s*(?:iframe|object|embed|base)\b)").unwrap();
    if dangerous.is_match(&stripped) {
        return Err("dynamic network or obfuscation is not allowed");
    }
    let urls = Regex::new(r#"(?i)(?:https?:)?//[^\s"'<>)}]+"#).unwrap();
    for found in urls.find_iter(&stripped) {
        let value = found.as_str();
        let normalized = if value.starts_with("//") {
            format!("https:{value}")
        } else {
            value.to_owned()
        };
        let url = url::Url::parse(&normalized).map_err(|_| "invalid URL")?;
        if !url
            .host_str()
            .is_some_and(|host| allow_hosts.contains(&host))
            || !url.username().is_empty()
            || url.password().is_some()
        {
            return Err("external URL is not allowed");
        }
    }
    Ok(())
}
