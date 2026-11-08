//! Basic validation for generated Motion HTML. This is not a sandbox.

use regex::Regex;

pub const MAX_BYTES: usize = 256 * 1024;

pub fn validate(html: &str, allow_hosts: &[&str]) -> Result<(), &'static str> {
    if html.len() > MAX_BYTES {
        return Err("slide exceeds size limit");
    }
    let root = Regex::new(r#"(?i)\bid\s*=\s*(?:"root"|'root'|root(?:\s|>))"#).unwrap();
    if !root.is_match(html) {
        return Err("slide must contain a root element");
    }
    let urls = Regex::new(r#"(?i)(?:https?:)?//[^\s"'<>)}]+"#).unwrap();
    for found in urls.find_iter(html) {
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
        {
            return Err("external URL is not allowed");
        }
    }
    Ok(())
}
