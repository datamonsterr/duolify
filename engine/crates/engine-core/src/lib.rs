use std::collections::BTreeMap;

use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct Models {
    pub tasks: BTreeMap<String, Model>,
}

#[derive(Debug, Deserialize)]
pub struct Model {
    pub model: String,
    pub fallback_model: String,
}

pub mod slide;

#[cfg(test)]
mod tests;
