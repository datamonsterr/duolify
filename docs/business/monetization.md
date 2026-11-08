# Monetization proposal

Decision date and price retrieval date: **2026-11-08**. Currency: USD, excluding transaction taxes. These are proposed launch prices and planning assumptions, not observed demand or approved commercial terms. E12 is the epic identifier in [the epic index](../epics/README.md).

Recommend a **$0.01 list value per Credit**, with at most **$0.0015 of protected AI cost per spent Credit**, **46 expiring daily free Credits**, three packs and three subscription tiers. Generation is paid; the daily bucket funds Tutor chat and Quick notes. At the base assumptions, monthly operating break-even is **9,103 MAU**, first reached in **month 10**. Cumulative operating losses are **$23,007.35** at year end; the peak operating deficit is **$32,897.31** in **month 9**, before deferred-credit liquidity reserves. This is a contribution model before corporate taxes and financing, not a cash-flow or revenue-recognition forecast.

## Official price evidence

Every source below was retrieved on **2026-11-08**. Rates are public list prices without negotiated discounts; recheck before launch. Source IDs used in subsequent tables refer to these exact URLs.

| ID | Official source | Observed price or rule |
|---|---|---|
| S1 | [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing) | Sonnet 5.5: input $2, output $10, five-minute cache write $2.50, one-hour write $4, cache hit $0.10 per million tokens. Haiku 5.5 up to 100,000 prompt tokens: $0.10/$0.50/$0.125/$0.20/$0.01 respectively; above that boundary: $0.50/$2.50/$0.625/$1/$0.05. |
| S2 | [Anthropic prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) | Five-minute writes cost 1.25 times ordinary input; one-hour writes cost twice input. Haiku 5.5 hits cost 0.1 times input; Sonnet 5.5 hits cost 0.05 times input. Both require at least 512 cached tokens. |
| S3 | [OpenRouter models API](https://openrouter.ai/api/v1/models) | `anthropic/claude-sonnet-5.5` and `anthropic/claude-haiku-5.5` have the same token/cache prices as S1. API units are USD/token; multiply by 1,000,000 for USD/million tokens. [Selected JSON response](openrouter-prices-2026-11-08.json) preserves IDs, canonical versions, prices and long-context overrides. |
| S4 | [OpenRouter pricing](https://openrouter.ai/pricing) | Standard prepaid funding fee 5.5%; Business 8%; no inference markup. Use Standard in this model. |
| S5 | [ElevenLabs API pricing](https://elevenlabs.io/pricing/api) | Flash/Turbo and v4 Turbo: $0.04 per 1,000 characters; v4, v3 and Multilingual v2: $0.08 per 1,000 characters. Use non-promotional pay-as-you-go character pricing, with no subscription commitment or grant discount assumed. |
| S6 | [ElevenLabs models](https://elevenlabs.io/docs/overview/models), [speech with timing](https://elevenlabs.io/docs/api-reference/text-to-speech/convert-with-timestamps) | Recommend `eleven_flash_v2_5` with character alignment for short learning narration, subject to voice/language evals. The timing endpoint returns character timestamps. |
| S7 | [Stripe US pricing](https://stripe.com/pricing?locale=en-US), [Billing pricing](https://stripe.com/billing/pricing) | Domestic cards: 2.9% + $0.30; international cards add 1.5%; currency conversion adds 1%. Checkout is included; Billing adds 0.7% of recurring volume. US merchant assumption below is essential: localized Stripe pages can show different rates. |
| S8 | [Apple Small Business Program](https://developer.apple.com/app-store/small-business-program/), [subscription proceeds](https://developer.apple.com/app-store/subscriptions/) | Eligible enrolled small businesses pay 15%; standard subscription proceeds start at 70% and become 85% after a year of paid service. Model 30% as adverse store commission. Eligibility depends on associated accounts and proceeds. |
| S9 | [Google Play service fees](https://support.google.com/googleplay/android-developer/answer/112622?hl=en) | In the US and other named rollout markets, subscriptions are 10% + 5% billing; standard other transactions for new installs are 20% + 5%, and existing installs 25% + 5%. First-$1M tier enrollment and eligibility matter. Other markets retain 15% on eligible first-$1M earnings, 30% above, and 15% auto-renewing subscriptions until rollout. Do not apply one rate globally. |
| S10 | [Apple review rules](https://developer.apple.com/app-store/review/guidelines/#in-app-purchase), [Google Play payments policy](https://support.google.com/googleplay/android-developer/answer/9858738?hl=en) | Digital purchases require the appropriate store billing unless a specific regional/program exception applies. Apple-purchased Credits may not expire. This proposal gives every paid bucket unlimited rollover across channels to avoid conflicting balances. |
| S11 | [Railway pricing](https://railway.com/pricing) | Pro minimum $20/month includes usage. RAM $0.00000386/GB-second; CPU $0.00000772/vCPU-second; volumes $0.00000006/GB-second; service egress $0.05/GB; object storage $0.015/GB-month with free egress. Railway has no published per-MAU tariff. |
| S12 | [Stripe fulfillment](https://docs.stripe.com/checkout/fulfillment.md?ui=full-page), [subscription webhooks](https://docs.stripe.com/billing/subscriptions/webhooks) | Fulfill from verified server events and paid status, including delayed payment completion; use paid invoices for recurring grants. A browser redirect is not proof of payment. |

The primary IDs in [models.toml](../../engine/config/models.toml) are `claude-sonnet-5-5` and `claude-haiku-5-5`. **The configured OpenRouter fallback IDs with hyphens were absent from S3**; the decimal IDs above were present. Costing those corresponding models is justified by their official names, but launch requires routing verification and a separate config correction. This work does not silently change the model selection. An OpenRouter route to Anthropic is not independent protection against an Anthropic-wide outage; after bounded retries, fail gracefully rather than inventing an unevaluated fallback.

Provider credits and Duolify Credits are different units. S5 currently quotes dollars per character directly; no legacy ElevenLabs credit-to-character exchange or introductory promotion is used. Premium licensed voice surcharges are excluded; only voices without a surcharge can be entitled at the standard rate.

## Assumptions and rationale

All unsourced values below are explicit planning choices. Measurements must replace them after launch; none is represented as vendor fact.

| ID | Assumption | Value and rationale |
|---|---|---|
| A1 | Launch market and currency | US adult self-directed learners, English first, USD; a Stripe-supported US merchant and domestic USD cards. This makes the fee model concrete. Vietnam and other local currencies need a separate willingness-to-pay and merchant review. Taxes are collected in addition and excluded from revenue; no FX or cross-border fees in the base case. |
| A2 | Learner behavior | Paid learner: 12 learning days/month, 0.5 newly generated Lessons/day = 6/month; 1 new Course/quarter = 1/3 per month, each with a paid preview and outline. Cached replays are unmetered. Free active days/month: conservative 20, base 10, optimistic 8. These are hypotheses about habit frequency, not benchmarks. |
| A3 | Lesson shape | 8 Slides, 180 narration characters/Slide = 1,440 characters/Lesson. Budget 8,000 Sonnet input tokens and 14,000 billed output tokens, including any thinking. Output allocation: 9,600 Motion HTML, 480 narration text, 1,400 test, 1,800 retry variants and 720 manifest/other tokens. Five test questions with two pre-generated variants each; reuse the pool after depletion without new AI calls. Narration tokens assume 3 characters/token; measure each language rather than relying on this ratio. |
| A4 | Preview and outline | Preview: 2,000 input + 2,500 output tokens, a small sample with 2 Slides and no audio. Outline: 4,000 input + 6,000 output tokens, no full Lesson content. No speculative future Lesson generation: Lesson N+1 only after passing Lesson N. The preview is a paid custom generation; a shared cached demo is available without AI spend. |
| A5 | Tutor context | A 12,000-token cacheable canonical course context (outline, summaries of all generated Lessons, learner profile and conversation history snapshot). Short: 500 fresh input/250 output; long: 1,000/650. Context access covers the whole Course through retrieval of its source content; do not silently drop old Lessons. Token budgets include retrieval/summary overhead. Cap total prompt at 48,000 tokens to stay below the S1 expensive Haiku context bracket; unusually large Courses require a separately costed context strategy. |
| A6 | Cache and routing | 80% of Tutor requests hit the five-minute cache; 20% write the prefix. Treat every miss as a paid write, not as a free hit. No cache savings for preview/outline/Lesson/notes. 10% of LLM spend routes through OpenRouter Standard, adding 5.5% to that portion: routing factor = 1 + 0.10 × 0.055 = 1.0055. |
| A7 | Notes and daily behavior | Quick note: 1,200 Haiku input + 300 output tokens, including instruction/links/tags/summary; no paid web-search tool. Active free day uses 3 notes plus either 8 short or 5 long messages. Use the more expensive combination for all free users' cost. Every paid learner also receives the same daily bucket; paid AI budget below accounts for their free interactions separately at A2's 12 active days/month. |
| A8 | Reliability allowance | Multiply LLM list cost by 1.20 for repair, retries, context maintenance, evaluation amortization and failed direct-provider work; multiply TTS by 1.10 for regeneration/alignment failures. These are expense reserves, not guaranteed discounts or extra consumer charges. No cross-learner narration deduplication savings assumed. |
| A9 | Credit economics | $0.01 list value/Credit; protected AI cost ceiling $0.0015/spent Credit. Target at least 70% paid AI gross margin after channel fees/refunds, before free-user subsidy, infra and fixed overhead. Service minimums for small actions pay for orchestration and keep user prices simple. Prices are proposed, not sourced competitor prices. |
| A10 | Paid mix | 90% subscription payers and 10% pack-only payers; no overlap or extra pack revenue assumed. Subscriber mix: 30% Learn, 50% Plus, 20% Pro; 70% monthly billing/30% annual, same mix by channel. Pack-only payer buys one $15 pack/month. Annual revenue and transaction fees amortized over 12 service months, steady billing-month distribution. |
| A11 | Redemption | Average subscriber monthly issuance = 0.30 × 600 + 0.50 × 1,600 + 0.20 × 3,600 = 1,700 Credits. Pack-only issuance = 1,350. Paid usage factor: conservative 100%, base 80%, optimistic 70%; expense budget includes paid learners' daily free AI. Never assume unused paid Credits expire or turn into profit. Base paid usage budget = (0.90 × 1,700 + 0.10 × 1,350) × 0.80 × $0.0015 = $1.998/payer/month for paid Credit redemption, plus 12 × $0.0064890948 = $0.0778691376 for the payer's daily free interactions, totaling $2.0758691376. |
| A12 | Acquisition/growth | Each scenario starts at 1,000 MAU. Net monthly MAU growth (already after churn): conservative 15%, base 30%, optimistic 50%. Paying fraction of current MAU: 2%, 5%, 8%, respectively; free fraction is the complement. These are scenario inputs, not forecasts. No additional churn subtraction, advertising revenue or viral credit rewards. |
| A13 | Channels/refunds | Web revenue and payer share: conservative 50%, base 70%, optimistic 80%; the rest uses store billing. Store fee: conservative 30%, base/optimistic 15%, contingent on eligibility and subscription-heavy mix. Refund reserve 2% of gross revenue in all cases; retain full original processing fees as a prudent expense. Subscription web percentage = 2.9% + 0.7% = 3.6%; pack web percentage = 2.9%; each transaction adds $0.30. |
| A14 | Variable infra | $0.04/free MAU/month and $0.12/paid MAU/month, planning allowances above the shared baseline. At S11 rates, free allocation: 0.001 GB-month RAM, 0.0005 vCPU-month, 0.02 volume GB-month, 0.1 service-egress GB, 0.1 object-storage GB-month = $0.02962, rounded up to $0.04. Paid allocation: 0.003 GB-month RAM, 0.0015 vCPU-month, 0.10 volume GB-month, 0.5 service-egress GB, 0.2 object-storage GB-month = $0.10358, rounded up to $0.12. Use 30 days × 86,400 seconds for these conversions. Validate with Railway invoices/load tests; they are not Railway quotes. |
| A15 | Fixed operations | $6,000/month: $200 shared Railway/DB/Redis/backup baseline, $400 other tools/monitoring/support budget, $5,400 founder labor and acquisition budget. The $200 baseline includes the S11 Pro minimum, not another $20 on top. These are owner-budget placeholders; no additional employee salary, financing or large paid-acquisition campaign is assumed. |
| A16 | Paid consumption mix for sensitivities | Paid Credit-redemption AI budget split in the ratio of one Lesson's protected text/audio costs: 74.8161% LLM, 25.1839% TTS, computed below. Daily free AI for both free and paid learners is entirely LLM. This is an expense allocation proxy; it is not a measured token distribution. |

All users may replay already generated Motion HTML and audio without another AI charge. Original generation pays for narration text and the initial retry pool; narration synthesis is a separate quoted component. Replays do not generate video. A learner buying another voice explicitly approves its character-based quote.

## Unit costs

Use S1–S6 and A3–A8. Tokens below are **billed tokens**, not estimated word counts. The cost equation is:

`LLM USD = (uncached_input × input_rate + cache_write × write_rate + cache_read × hit_rate + output × output_rate) / 1,000,000`.

Protected LLM expense = list cost × 1.20 × 1.0055 = list cost × **1.2066**. Protected narration = characters × $0.04/1,000 × 1.10. Output includes reasoning if billed. Tools such as web search, image generation, speech input and voice cloning are excluded and must have separate quotes before enabling them.

| Action | Base calculation, USD | Protected cost, USD |
|---|---|---:|
| Course preview | (2,000 × 2 + 2,500 × 10)/1,000,000 = 0.029000 | 0.03499140 |
| Course outline | (4,000 × 2 + 6,000 × 10)/1,000,000 = 0.068000 | 0.08204880 |
| Lesson content, narration text, test and retry pool | (8,000 × 2 + 14,000 × 10)/1,000,000 = 0.156000 | 0.18822960 |
| Narration audio/Lesson | 1,440 × 0.04/1,000 = 0.057600 | 0.06336000 |
| Complete newly generated Lesson | Text + audio | 0.25158960 |
| Tutor short, warm hit | (12,000 × 0.01 + 500 × 0.10 + 250 × 0.50)/1,000,000 = 0.000295 | 0.0003559470 |
| Tutor short, cold write | (12,000 × 0.125 + 500 × 0.10 + 250 × 0.50)/1,000,000 = 0.001675 | 0.0020210550 |
| Tutor short, blended | 0.80 × 0.000295 + 0.20 × 0.001675 = 0.000571 | 0.0006889686 |
| Tutor long, warm hit | (12,000 × 0.01 + 1,000 × 0.10 + 650 × 0.50)/1,000,000 = 0.000545 | 0.0006575970 |
| Tutor long, cold write | (12,000 × 0.125 + 1,000 × 0.10 + 650 × 0.50)/1,000,000 = 0.001925 | 0.0023227050 |
| Tutor long, blended | 0.80 × 0.000545 + 0.20 × 0.001925 = 0.000821 | 0.0009906186 |
| Quick note enrichment | (1,200 × 0.10 + 300 × 0.50)/1,000,000 = 0.000270 | 0.0003257820 |

If an endpoint does not support caching, a short uncached call costs $0.001375 and a long one $0.001625 before protection, by replacing the cached prefix's rate with S1 input. A five-minute cold write can cost more than that uncached request; the blended budget does not pretend otherwise. One-hour writes are available at S1 rates, but are not assumed beneficial for short daily sessions. Track write and hit tokens separately.

At A7 usage, free-day expense is `max(8 × 0.0006889686, 5 × 0.0009906186) + 3 × 0.0003257820` = **$0.0064890948**. Base free MAU expense is that × 10 = **$0.064890948/month**. At 30 full-use days it is **$0.194672844/month**. These are AI costs, separate from A14 infra.

## Credit catalogue and free allowance

The following are proposed policy values, justified by A9's cost ceiling and a small service minimum. Credit prices are held constant for a valid quote. Standard actions have the token/character limits above; larger requests need a newly disclosed quote. Credits are a service unit and cannot be cashed out. The $0.01 list value is not a promise that every SKU has the same exchange rate.

| Action | Credits for standard envelope | Protected USD/Credit | Included scope |
|---|---:|---:|---|
| Course preview | 25 | 0.00139966 | Sample outline and 2 Slides, text only |
| Course outline | 55 | 0.00149180 | Outline only; no prepaid future Lessons |
| Lesson content | 140 | 0.00134450 | 8 Slides, narration text, test and retry pool |
| Narration synthesis | 50 for 1,440 newly synthesized characters | 0.00126720 | Config rate is the rational 50/1,440 Credits per character; ceil once across all new characters in the request |
| Complete standard Lesson | 190 = 140 + 50 | 0.00132416 | Both content and audio; show both components before approval |
| Tutor short | 5 | 0.00013779 | A5 short envelope, including course context |
| Tutor long | 8 | 0.00012383 | A5 long envelope, including course context |
| Quick note enrichment | 2 | 0.00016289 | A7 input/output envelope |
| Cached replay / existing narration hash | 0 | 0 AI | No new generation; delivery cost is in infra |

Daily free grant: **46 Credits**, from `3 × 2 + 8 × 5 = 3 × 2 + 5 × 8`. It expires at the end of the Learner's local day, never rolls over and is consumed before paid Credits for eligible chat/notes. Short and long message limits are visible before sending. A mixed day fits when `2 × notes + 5 × short + 8 × long ≤ 46`. This is a fungible budget, not separate mandatory quotas; fewer notes allow more chat. Custom generation and narration use paid buckets, preventing the much more expensive preview from replacing the free-cost assumption. A shared demo supports free discovery without a per-user generation subsidy.

No effort, streak, badge, perfect test, referral milestone or Friend streak awards Credits. Streak freezes are earned by effort and auto-applied. Paid benefits neither issue freezes nor purchase a streak repair. Paid subscribers still receive the same daily free grant.

Implementation pricing equation: `settled Credits = min(approved quote ceiling, max(feature service minimum, ceil(actual billable provider USD / 0.0015)))` for token-priced actions; narration instead uses its character rate. The minimums are the standard action prices above. Report actual token classes and provider expense separately; do not claim cached requests have the same cost as misses. Bounded standard jobs reserve their displayed price and settle that minimum upon successful delivery. Failed jobs release the entire reservation. Out-of-envelope work must stop or request a new quote; retries and operator repairs cannot silently exceed the approved ceiling. Customer-caused explicit new generation is a new action. The exact price table, service minimums, ceilings, token limits and rounding policy live in config, never in engine logic.

## Packs and subscriptions

Prices below are A9/A10 commercial hypotheses. Paid Credits **never expire**, survive cancellation/downgrade, and roll over without a balance cap. Spend expiring daily Credits first, then the oldest eligible paid bucket. Keep provenance for refunds and merchant reporting. Credit balances are liabilities; do not use expiry or assumed breakage to finance the model.

| One-time pack | USD | Credits | Effective USD/Credit | Rationale |
|---|---:|---:|---:|---|
| Try | 5 | 400 | 0.012500 | Low entry price with room for the fixed payment fee |
| Build | 15 | 1,350 | 0.011111 | A10 reference pack for occasional generation |
| Deep | 40 | 4,000 | 0.010000 | List-value bulk pack; preserves contribution margin |

| Plan | Monthly USD | Yearly USD, prepaid | Monthly equivalent if annual | Credits each service month | Non-credit benefits |
|---|---:|---:|---:|---:|---|
| Learn | 9.99 | 96 | 8 | 600 | Offline course library downloads; no upsell banners; learning history export |
| Plus | 19.99 | 192 | 16 | 1,600 | Learn benefits; priority generation queue; advanced learning analytics; curated voices at the same synthesis rate |
| Pro | 39.99 | 384 | 32 | 3,600 | Plus benefits; custom learning analytics exports; higher concurrent generation limit |

All tiers grant monthly, including annual subscriptions; never grant the whole year's allowance at purchase. Monthly issuance is keyed to the billing anniversary with a documented month-end clamp. Annual payment funds the scheduled grants for its paid term even after auto-renew is turned off. Existing granted Credits survive term end. Unlimited playback of already generated Lessons is a universal product capability; offline library convenience and other plan entitlements do not promise unlimited AI generation. Family sharing is deferred until per-seat cost and consent rules are validated; these tiers are individual plans.

Using A2, six standard Lessons plus one-third of a preview/outline consume `6 × 190 + (25 + 55)/3` = **1,166.67 Credits/month** before paid chat above the free allowance. Plus fits that illustrative habit with room for extras; Learn fits lighter usage. Pro's standard Lesson-only allowance is `floor(3,600/190)` = **18 Lessons/month**, so a daily new Lesson habit needs packs. Do not advertise all plans as unlimited or as guaranteeing a newly generated Lesson every day.

Paid AI margin is calculated on net receipts: `1 − protected AI cost / (sale price − fees − refunds)`. At full redemption, the tightest token-action cost/Credit is the outline's $0.00149180, below A9's $0.0015 ceiling. The cheapest subscription exchange rate is Pro annual: `384/(12 × 3,600)` = **$0.00888889/Credit**. With a 30% store fee and the 2% refund reserve, receipts are $0.00604444/Credit; at the ceiling, paid AI margin is **75.18%**, above the 70% target. Other SKUs must pass the same test including fixed Stripe charges. This margin excludes free users and infra; company contribution margins below are lower. Future discounted prices must pass this calculation before activation.

### Positioning evidence

These observations were retrieved on **2026-11-08** and do not set Duolify's costs. Storefront offers can differ by region/account and include legacy SKUs. Never infer billing duration where the official listing does not label it.

| Product | Official price observation | Positioning implication |
|---|---|---|
| Duolingo Super | [US developer App Store listing](https://apps.apple.com/us/app/duolingo-learn-languages/id570060128) shows Super SKUs including $9.99, $12.99 and $83.99; duration is not identified there. | Learn's monthly sticker sits in the observed entry-price range; Duolify must explain its generation allowance. |
| Duolingo Max | [Official Max description](https://blog.duolingo.com/duolingo-max/) describes AI features above Super; [public Max page](https://www.duolingo.com/max) exposes no readable price and the US listing does not show a Max SKU. **Current official USD price unverified.** | Compare the AI tutoring proposition only; do not repeat historical Max prices as current. Confirm in the target-market checkout before launch. |
| ChatGPT Plus | [Official OpenAI documentation](https://learn.chatgpt.com/docs/pricing) lists $20/month for Plus. | Plus competes at approximately the same monthly price through a structured, persistent learning experience. ChatGPT subscription usage is not an API price. |
| Speak | [US developer App Store listing](https://apps.apple.com/us/app/speak-language-learning/id1286609883) explicitly labels Monthly Premium $17.99, Annual Premium $83.99, Monthly Premium Plus $39.99 and Annual Premium Plus $164.99. | AI conversation is established at Plus/Pro-like monthly stickers; Duolify's annual offers are less aggressively discounted and must prove cross-topic value. |
| Brilliant | [US developer App Store listing](https://apps.apple.com/us/app/brilliant-learn-math-coding/id913335252) shows Premium $24.99/$149.99 and Premium with Tutor $191.99, without durations. [Direct subscription page](https://brilliant.org/subscribe/) localized to KRW on retrieval. | Curated deep-learning content and tutoring anchor a premium comparison, but a verified US checkout is needed for a like-for-like annual comparison. No exchange-rate conversion assumed. |

## Twelve-month business model

[model.csv](model.csv) contains **36 computed rows**, one for each of 12 service months in each of 3 scenarios. There are no spreadsheet expressions. Its header encodes units: `_usd` is monthly dollars except the explicitly cumulative column; `_fraction` is a decimal proportion; `_users` is a count or expected count; `month_index` starts at 1. Counts of paid/free users are fractional cohort expectations, not rounded invoices. Only MAU is rounded to the nearest whole user, halves up. Monetary outputs retain 8 decimal places; display tables round to cents or the stated precision.

The months are relative to launch, not calendar claims. A12 provides the net growth curve: `MAU_m = round(1,000 × (1 + growth)^(m−1))`; paid users = MAU × conversion; free users = MAU − paid users.

From A10 and the plan table:

- Weighted monthly plan price = `0.30 × 9.99 + 0.50 × 19.99 + 0.20 × 39.99` = **$20.99**.
- Weighted annual monthly equivalent = `0.30 × 8 + 0.50 × 16 + 0.20 × 32` = **$16.80**.
- Subscriber recognized revenue/month = `0.70 × 20.99 + 0.30 × 16.80` = **$19.733**.
- **ARPPU = 0.90 × 19.733 + 0.10 × 15 = $19.2597** in all scenarios. It is not an arbitrary additional input.
- Subscription web transactions per service month = `0.70 + 0.30/12` = **0.725**, fixed-fee equivalent **$0.2175**. Pack-only payers have one transaction/month. Web subscriber fee = `19.733 × 0.036 + 0.2175`; web pack fee = `15 × 0.029 + 0.30`. Store fees replace those web fees for that channel; never stack Stripe and store commissions.
- Fee per payer = `0.90 × [web_share × subscriber_web_fee + (1−web_share) × 19.733 × store_rate] + 0.10 × [web_share × pack_web_fee + (1−web_share) × 15 × store_rate]`.
- Gross revenue = paid users × ARPPU. Refund reserve = revenue × 0.02. Free AI = free users × active days × $0.0064890948. Paid AI = paid users × `[(0.90 × 1,700 + 0.10 × 1,350) × utilization × 0.0015 + 12 × 0.0064890948]`. Paid Credit redemption uses an expense ceiling, not a claim that all features cost $0.0015/Credit; daily free interactions of paid users are added explicitly. The CSV separates `paid_credit_ai_usd_per_paid_user`, `paid_daily_allowance_ai_usd_per_paid_user` and the combined `paid_ai_budget_usd_per_paid_user`.
- Variable infra = free users × $0.04 + paid users × $0.12. Contribution = revenue − refunds − fees − free AI − paid AI − variable infra. Contribution margin = contribution/revenue. Operating result = contribution − $6,000. Cumulative operating result sums those monthly results.
- Contribution per MAU = `conversion × (ARPPU − fees_per_payer − 0.02 × ARPPU − paid_AI_per_payer − 0.12) − (1−conversion) × (free_AI_per_free + 0.04)`. There is no free-user revenue. Break-even MAU = `ceil(6,000 / contribution_per_MAU)` if positive; otherwise no finite break-even. This threshold assumes fixed cost and cohort/channel mix remain constant as scale grows.

| Scenario | Monthly growth | Paid conversion | Month-12 MAU | Free AI/free MAU/month | Month-12 revenue | Month-12 contribution margin | Month-12 operating result | Break-even MAU / first profitable month | Cumulative year operating result |
|---|---:|---:|---:|---:|---:|---:|---:|---|---:|
| Conservative | 15% | 2% | 4,652 | $0.129781896 | $1,791.92 | 23.45% | −$5,579.78 | 66,423 / not in modeled year | −$69,380.21 |
| Base | 30% | 5% | 17,922 | $0.064890948 | $17,258.62 | 68.45% | $5,813.29 | 9,103 / month 10 | −$23,007.35 |
| Optimistic | 50% | 8% | 86,498 | $0.0519127584 | $133,274.04 | 75.63% | $94,799.37 | 5,149 / month 6 | $228,067.44 |

Base month-12 expense bridge (CSV, rounded): fees **$1,346.57**, refund reserve **$345.17**, free AI **$1,104.83**, paid AI **$1,860.19**, variable infra **$788.57**; contribution **$11,813.29**, less **$6,000** fixed cost. The base contribution per MAU is **$0.65915014552**. Monthly break-even is not cumulative cash recovery: the base year ends in an accumulated deficit even though later months are profitable. The minimum base cumulative-result row is −$32,897.31 in month 9; budget at least that operating runway, plus payment-timing and outstanding-credit reserves.

Annual prepayments and one-time packs create deferred service obligations. The CSV spreads annual receipts and pack revenue over expected monthly service/consumption for comparability; it does not treat unredeemed balances as legally recognized revenue. It has no opening rollover balances. Carry unused paid Credits indefinitely in the actual ledger, measure monthly issuance/redemption and deferred revenue separately, and obtain an accounting policy before launch. The conservative scenario assumes full monthly issuance redemption; the sensitivity below tests that on the base cohort, in addition to paid learners' free usage. Redemption of accumulated old balances can exceed current issuance; maintain a separate outstanding-credit cost reserve of **outstanding paid Credits × $0.0015**, funded before distributions, and rerun the model against observed redemption cohorts. This reserve is a liquidity requirement, not an extra expense to double-count against the same usage.

### Sensitivity and viability limits

Hold all other base inputs fixed. A16 allocates paid Credit-redemption expense between LLM and TTS; daily free AI for both paid/free learners is entirely LLM. New break-even is `ceil(6,000 / [0.65915014552 − 0.05 × change_in_paid_AI_per_payer − 0.95 × change_in_free_AI_per_free])`. For the store case substitute fee change for paid AI change. These are model reruns, not predictions.

| Change from base | Break-even MAU | Reason |
|---|---:|---|
| LLM prices rise 50% | 10,187 | Increase all daily free AI by half and paid Credit-redemption AI by half its A16 LLM allocation |
| LLM prices fall 50% | 8,228 | Symmetric saving; retain prices pending quality and demand review |
| TTS price doubles to S5's $0.08/1,000 characters | 9,464 | Double only the paid TTS allocation |
| Paid usage rises from 80% to 100% of issuance | 9,462 | Paid Credit-redemption AI increases from $1.998 to $2.4975/payer/month, plus unchanged paid-user daily free AI |
| Free use reaches 30 active days/month | 11,198 | Free AI triples from the base 10-day assumption |
| Store commission rises from 15% to 30% | 9,744 | Additional fee/payer = 30% store share × $19.2597 × 15% |
| All AI prices double | 12,153 | Double both paid and free AI expense; SKU margins require repricing review |

Cold-cache stress is computed separately: all short messages writing gives `8 × 0.002021055 + 3 × 0.000325782` = **$0.017145786/free active day**, rather than $0.0064890948. At 30 days, **$0.51437358/free MAU/month**. Track this explicitly: high free usage, low conversion and weak caching together can erase contribution. On the base cohort, cold caches and 30-day free use reduce contribution per MAU to **$0.23214164512/month**, implying **25,847 MAU** break-even, before any further paid-user cache stress. The daily grant still covers the promised actions; service must queue or disable new admissions when a platform safety budget is hit rather than quietly reduce grants for existing learners.

The paid 70% margin target is not guaranteed after arbitrary price increases: twice the AI ceiling at the cheapest annual SKU and adverse fees gives `1 − 0.003/0.00604444` = **50.37%**. Review new quotes/catalogue prices, select a cheaper evaluated model, or pause selling the affected SKU; do not retroactively devalue issued Credits or paid plan terms. Conversion, free-day frequency and infra allowance require observation as urgently as model prices.

## Guardrails, analytics and operational policy

The numerical controls in this section are **proposed launch policy**, not provider defaults. Their rationale is to bound losses while preserving A7's free promise and allowing ordinary paid habits.

- Per-free-learner cost cap: **$0.08/local day**. This exceeds both the standard cold-cache day at $0.017145786 and a maximum-context cold-cache short-message day: `8 × [(48,000 × 0.125 + 500 × 0.10 + 250 × 0.50)/1,000,000 × 1.2066] + 3 × 0.000325782` = **$0.060583386**, preserving the daily promise up to A5's context cap. Paid per-learner cap: **$5/local day**, configurable opt-in operator raise after fraud review. Bound each generation request to **$0.50** total provider expense; complete standard Lesson protection is $0.25158960. Stop provider work at cost/output/time limits and release failed reservations. Do not rely on credit exhaustion alone for these USD controls.
- Verification and abuse: authenticated verified accounts; **1** daily grant per Learner/local date, atomic uniqueness, device/IP risk signals and challenge for suspicious account farms. Rate limit paid generation to **1** concurrent job on Learn/Plus and **2** on Pro, plus **1** free Tutor stream, queuing legitimate requests. Timezone changes take effect only after **24 hours** with an elapsed-time grant guard so travel cannot farm grants; the daily reset follows the stored IANA timezone, including daylight-saving changes. These are duplicate-prevention controls, not reductions to the daily allowance.
- Reliability: paid generation deadline **180 seconds**, Tutor/notes **30 seconds**, TTS **60 seconds**; at most **1** repair/fallback after the primary attempt and within the original USD/quote ceiling. If alignment fails, retain the text Lesson, release/refund only the undelivered audio component, and retry with the same content hash. No repeated charge for an already delivered artifact. Honor gated generation and reuse retry variants without metering another AI call.
- PostHog alarms: notify at **80%** and **100%** of the configured platform daily AI budget, initially **$50/day** (a launch loss tolerance, deliberately not an estimate of steady-scale cost). Trigger review at a **20%** increase in rolling **7-day** cost/active learner versus forecast, cache hit fraction below **60%**, or paid AI margin below **70%**. Scale the absolute budget with the adopted scenario and runway before admitting growth. Platform enforcement uses Postgres/Redis counters and provider usage reconciliation; analytics opt-out and a PostHog outage must not remove spend protection.
- Event contract: extend the canonical analytics catalogue before implementation. Emit `paywall_viewed` (placement, feature, balance, plan_id, price_table_version), `checkout_started` (sku_id, channel, currency, amount_minor), `plan_purchased` (plan_id, interval, channel, amount_minor, currency, billing_period_start/end), `subscription_renewed`, `subscription_cancelled`, `subscription_payment_failed` (plan_id, channel, reason_code), `subscription_grant_issued` (amount, period_key), `credits_exhausted` (feature, bucket, balance, estimate), `credits_refunded` (amount, payment_reference, reason_code), `entitlement_changed` (plan_id, entitlement_version), `price_table_activated` (old_version, new_version), `ai_cost_budget_alert` (scope, actual_usd, budget_usd) and `payment_reconciliation_failed` (channel, reason_code). Lifecycle events use the corresponding plan/interval/period properties; balance events include price version and bucket. Existing `credits_reserved/settled/released/insufficient/purchased/purchase_failed` and `$ai_generation` remain in use.
- Every event inherits the analytics spec's UUID identity, platform, app_version and source; include Course/Lesson IDs when known. Add aggregate token classes, provider/model, fallback, AI cost, latency, character counts and price version for cost events. Never send raw prompts, note/chat/profile text, HTML, payment card data or emails. Respect opt-out; financial records and server budget counters remain operational data. Non-blocking transactional outbox deduplicates financial event emission; PostHog is never the payment source of truth.
- Keep financial ledger, webhook dedupe keys, orders, invoices, refunds and historical price versions in Postgres for **at least 7 years** (existing credits retention requirement; extend for applicable law/legal hold). Pseudonymize identity on account deletion while retaining legally required records. Retain raw redacted webhook payloads **30 days** for debugging, derived analytics **90 days**, and encrypted daily backups **35 days**, proposed minimization defaults. Ledger and catalogue survive beyond snapshot rotation through the live database and fresh backups. Test restore plus replay/reconciliation **quarterly**; verify balances, paid grant uniqueness, entitlements and outstanding-credit liability against provider records. Delete expired raw data and apply account-deletion tombstones after restoring old snapshots. Narration/text assets follow course retention and account deletion policy, with no card data in object storage.

The [OpenSpec change](../../openspec/changes/monetization-model/proposal.md) makes these policies reviewable requirements. Future implementation must include Golden set usage, large-context, retry, multilingual narration and provider-failure cases at a pinned Hugging Face revision; it must pass the repo's mock and live evaluation gates before enabling a model/voice. This documentation change does not implement AI paths or claim a live evaluation run.

## Open questions

The model proceeds with the stated defaults; owner answers should version the proposal rather than block this deliverable.

1. **Which merchant entity, launch countries, age segment and currencies are intended?** A1 assumes US adults and USD; confirm whether Vietnam is primary and whether local payment methods, taxes and willingness to pay require a different catalogue.
2. **What learning habit and content quality should the subscription guarantee?** Confirm A2–A5's six paid Lessons/month, Slide/narration length, full-course context strategy, message envelopes and narration language/voice set. A daily-new-Lesson promise requires larger allowances or higher prices.
3. **What fixed budget, runway and growth evidence are acceptable?** Confirm A12–A15's conversion/growth, acquisition allocation and $6,000 monthly budget; the base year still has a cumulative deficit and excludes a larger team.
4. **Is the proposed free acquisition experience acceptable?** Daily 46 Credits cover the fixed notes/chat promise; custom previews/outlines/narration need paid Credits. Confirm a shared demo versus a separately budgeted onboarding subsidy; no credit rewards for effort will be introduced.
5. **Which billing channels and rollover liability can launch support?** Confirm Apple small-business enrollment, Google regional eligibility, non-expiring paid buckets, monthly annual-plan grants, refund policy and individual-only tiers. Verify Max and competitor checkout offers in the actual target storefront before positioning claims.
