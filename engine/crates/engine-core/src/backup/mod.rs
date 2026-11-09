//! Safe operational correlation shared by the engine's RPC boundary.
//! This module has no backup or model behavior; metadata never contains payloads.

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Correlation {
    pub request_id: String,
    pub trace_id: String,
    pub parent_span_id: String,
}

impl Correlation {
    pub fn parse(request_id: &str, traceparent: &str) -> Option<Self> {
        let valid_hex = |value: &str, size| {
            value.len() == size
                && value
                    .bytes()
                    .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
                && value.bytes().any(|b| b != b'0')
        };
        if request_id.len() != 26
            || !request_id
                .bytes()
                .all(|b| b.is_ascii_uppercase() || (b'2'..=b'7').contains(&b))
        {
            return None;
        }
        let parts: Vec<_> = traceparent.split('-').collect();
        if parts.len() != 4
            || parts[0] != "00"
            || !valid_hex(parts[1], 32)
            || !valid_hex(parts[2], 16)
            || parts[3].len() != 2
            || !parts[3].bytes().all(|b| b.is_ascii_hexdigit())
        {
            return None;
        }
        Some(Self {
            request_id: request_id.into(),
            trace_id: parts[1].into(),
            parent_span_id: parts[2].into(),
        })
    }
}

#[cfg(test)]
mod tests;
