//! Conservative static checks; a sandbox and restrictive CSP are still required at playback.
use regex::Regex;
pub const MAX_BYTES: usize = 256 * 1024;
const DANGEROUS: &str = r"(?i)(?:&(?:#|[a-z]+;)|\\|\b(?:fetch|websocket|eventsource|xmlhttprequest|sendbeacon|importscripts)\b|@import|\b(?:ftp|wss?|data|javascript):|<\s*(?:iframe|object|embed|base)\b)";

/// The first text that trips the obfuscation and network check, for a repair message.
pub fn dangerous_match(html: &str) -> Option<String> {
    let stripped = Regex::new(r"(?s)<!--.*?-->").unwrap().replace_all(html, "");
    Regex::new(DANGEROUS)
        .unwrap()
        .find(&stripped)
        .map(|m| m.as_str().to_owned())
}

/// Elements whose content a browser reads as text until the closing tag.
const RAW_TEXT: [&str; 5] = ["textarea", "title", "noscript", "xmp", "noembed"];

pub fn validate(html: &str, allow_hosts: &[&str]) -> Result<(), &'static str> {
    if html.len() > MAX_BYTES {
        return Err("slide exceeds size limit");
    }
    let stripped = Regex::new(r"(?s)<!--.*?-->").unwrap().replace_all(html, "");
    let without_scripts = Regex::new(r"(?is)<script\b[^>]*>.*?</script\s*>")
        .unwrap()
        .replace_all(&stripped, "");
    let root =
        Regex::new(r#"(?i)<[a-z][a-z0-9-]*\b[^>]*\bid\s*=\s*(?:"root"|'root'|root(?:\s|>))"#)
            .unwrap();
    if !root.is_match(&without_scripts) {
        return Err("slide must contain a root element");
    }
    // A raw-text element left open makes a browser read the rest of the document as its text.
    let lower = stripped.to_ascii_lowercase();
    if lower.contains("<plaintext") {
        return Err("unclosed raw-text element is not allowed");
    }
    for tag in RAW_TEXT {
        if lower.matches(&format!("<{tag}")).count() > lower.matches(&format!("</{tag}")).count() {
            return Err("unclosed raw-text element is not allowed");
        }
    }
    // Reject obfuscation and dynamic network primitives rather than trying to interpret JS.
    let dangerous = Regex::new(DANGEROUS).unwrap();
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

#[cfg(test)]
mod tests {
    use super::validate;

    #[test]
    fn an_unclosed_raw_text_element_is_rejected() {
        for tag in ["textarea", "title", "noscript", "xmp", "noembed"] {
            let html = format!(r#"<div id="root"><{tag}>swallowed<p data-slot="a">x</p></div>"#);
            assert!(validate(&html, &[]).is_err(), "unclosed <{tag}>");
            let closed = format!(r#"<div id="root"><{tag}>kept</{tag}><p>x</p></div>"#);
            assert!(validate(&closed, &[]).is_ok(), "closed <{tag}>");
        }
        // plaintext never closes.
        assert!(validate(r#"<div id="root"><plaintext>x</div>"#, &[]).is_err());
        // An SVG title is closed and fine.
        assert!(
            validate(
                r#"<div id="root"><svg><title>Diagram</title><rect/></svg></div>"#,
                &[]
            )
            .is_ok()
        );
    }
}
