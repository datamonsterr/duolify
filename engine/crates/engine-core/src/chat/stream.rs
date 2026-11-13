//! Reconciles a tutor reply stream: typed errors and exactly one usage frame.
//!
//! A provider stream may end (or fail) before its final usage frame, and the API must still settle
//! what was delivered. [`reconcile`] forwards deltas, converts a failure into a typed [`Part::Error`]
//! and guarantees one usage record: the provider's own, or an estimate from the delivered bytes
//! bounded by the quote limits and flagged `estimated`. Dropping the returned stream drops the
//! provider stream, which is how a cancelled gRPC call stops provider work.
use std::collections::VecDeque;

use futures::{Stream, StreamExt};

use crate::eval::{StreamFailure, StreamRoute};
use crate::{Completion, CompletionStream, StreamEvent};

/// A typed streaming failure; the stream ends after it (preceded by one usage frame).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Failure {
    pub code: &'static str,
    pub retryable: bool,
}

#[derive(Debug, Clone)]
pub enum Part {
    Delta(String),
    Usage {
        completion: Completion,
        estimated: bool,
    },
    Error(Failure),
}

/// Token caps from the API's immutable quote; estimates never exceed them.
#[derive(Debug, Clone, Copy)]
pub struct Bounds {
    pub input_tokens: u32,
    pub output_tokens: u32,
}

fn tokens(bytes: usize) -> u32 {
    u32::try_from(bytes.div_ceil(4)).unwrap_or(u32::MAX)
}

pub fn failure_of(error: &anyhow::Error) -> Failure {
    match error.downcast_ref::<StreamFailure>() {
        Some(StreamFailure::FirstTokenTimeout) => Failure {
            code: "first_token_timeout",
            retryable: true,
        },
        Some(StreamFailure::IdleTimeout) => Failure {
            code: "idle_timeout",
            retryable: true,
        },
        Some(StreamFailure::DeadlineExceeded) => Failure {
            code: "deadline_exceeded",
            retryable: false,
        },
        None => Failure {
            code: "provider_error",
            retryable: true,
        },
    }
}

struct State {
    inner: Option<CompletionStream>,
    queue: VecDeque<Part>,
    route: StreamRoute,
    prompt_bytes: usize,
    bounds: Option<Bounds>,
    delivered: usize,
    usage_seen: bool,
}

impl State {
    fn estimate(&self) -> Part {
        let (mut input, mut output) = (tokens(self.prompt_bytes), tokens(self.delivered));
        if let Some(b) = self.bounds {
            input = input.min(b.input_tokens);
            output = output.min(b.output_tokens);
        }
        Part::Usage {
            completion: Completion {
                provider: self.route.provider.clone(),
                model: self.route.model.clone(),
                input_tokens: input,
                output_tokens: output,
                ..Completion::default()
            },
            estimated: true,
        }
    }
}

/// Wraps `inner` so it always ends with one usage frame and reports failures as typed parts.
pub fn reconcile(
    inner: CompletionStream,
    route: StreamRoute,
    prompt_bytes: usize,
    bounds: Option<Bounds>,
) -> impl Stream<Item = Part> + Send {
    let state = State {
        inner: Some(inner),
        queue: VecDeque::new(),
        route,
        prompt_bytes,
        bounds,
        delivered: 0,
        usage_seen: false,
    };
    futures::stream::unfold(state, |mut st| async move {
        loop {
            if let Some(part) = st.queue.pop_front() {
                return Some((part, st));
            }
            let inner = st.inner.as_mut()?;
            match inner.next().await {
                Some(Ok(StreamEvent::Delta(d))) => {
                    st.delivered += d.len();
                    return Some((Part::Delta(d), st));
                }
                Some(Ok(StreamEvent::Usage(completion))) => {
                    st.usage_seen = true;
                    return Some((
                        Part::Usage {
                            completion,
                            estimated: false,
                        },
                        st,
                    ));
                }
                Some(Err(error)) => {
                    st.inner = None;
                    st.queue.push_back(Part::Error(failure_of(&error)));
                    if !st.usage_seen {
                        let estimate = st.estimate();
                        st.queue.push_back(estimate);
                    }
                }
                None => {
                    st.inner = None;
                    if !st.usage_seen {
                        let estimate = st.estimate();
                        st.queue.push_back(estimate);
                    }
                }
            }
        }
    })
}
