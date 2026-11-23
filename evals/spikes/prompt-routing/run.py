#!/usr/bin/env python3
"""Throwaway spike: monolithic vs skill-routed lesson_slides prompt (openspec change
prompt-skill-routing-and-guardrails). Not wired into engine:eval, not a production path.
Key comes from OPENROUTER_API_KEY in the environment only; it is never printed or stored.
  python3 run.py --out results.json --budget 0.30
"""
import argparse, json, os, re, sys, time, urllib.request, urllib.error

URL = "https://openrouter.ai/api/v1/chat/completions"
DEC = "https://openrouter.ai/api/alpha/decisions"
MODEL = "tencent/hy4-flash"
ROUTER = os.environ.get("ROUTER_MODEL", "baseten/joey-1")
THRESH = float(os.environ.get("ROUTER_THRESHOLD", "0.5"))

# --- production text, copied from engine-core (lessontest::LESSON_SHAPE, regions::shape) -------
LESSON_SHAPE = 'Return one JSON object with title, slides (slide_id, html, narration, duration_ms, topic_ids), test and variant_pool (topic_id, variants). Keep it short: 2 to 4 slides teaching 1 or 2 topic_ids. Slide html has one root element id="root" and inline CSS only: no scripts, URLs, HTML entities or backslashes. narration is plain text; duration_ms is 3000 to 60000 and at least 400 per narration word. Test: one question per taught topic_id, unique question_ids, 2 to 6 different choices. variant_pool: one variant per tested topic with a new question_id, variant_of set to the test question_id, a reworded prompt, the same choice texts in a different order and answer_index on the same correct text.'
ROLES = ["title", "explanation", "example", "diagram", "code", "question", "caption"]
REGIONS = f' Lay out every slide on fixed slots of the 16:9 stage: top (title band), body (main area), left and right (its halves), left_top, left_bottom, right_top and right_bottom (its quarters) and bottom (caption band). Put each visible block in one element inside the root element (not inside another block) whose data-slot attribute is the slot name, for example <h1 data-slot="top">; use each slot at most once per slide. The player sizes and places that element in its slot, so do not position or size it, never use !important, and keep the text short enough to fit. Each slide lists regions (1 to 6), one per block: slot (the same name as its element\'s data-slot), role (one of {", ".join(ROLES)}; diagram for any figure, chart or labelled drawing, code for code, question for a question to the learner), description (plain text, at most 300 characters, no markup, in the slide\'s language, saying what the block shows and quoting its key words) and start_ms/end_ms on the slide clock when the block is visible only part of the slide, else 0 and 0. Blocks visible at the same time use slots that do not overlap (body overlaps left, right and the quarters; left overlaps left_top and left_bottom; right overlaps right_top and right_bottom).'
HEAD = "Generate a lesson with Motion HTML slides, explicit taught topic_ids and one question per distinct taught topic. Each question needs question_id, topic_id, prompt, choices, answer_index and explanation. Include genuine paraphrases in variant_pool linked by variant_of. "
ARM_A_RULES = HEAD + LESSON_SHAPE + REGIONS  # production prompt, rules first, context last

SKILLS = {  # id -> (router description, body)
 "diagram_layout": ("The lesson explains a process, structure, flow or labelled figure that is best drawn.",
  "Skill diagram_layout: draw the figure as inline SVG (viewBox 0 0 100 60) in the body slot, 3 to 6 labelled nodes, arrows with marker-end, one idea per slide, labels under 3 words. Region role diagram, description lists the node labels. Example: <svg data-slot=\"body\" viewBox=\"0 0 100 60\"><rect x=\"5\" y=\"20\" width=\"25\" height=\"15\"/><text x=\"8\" y=\"30\">Client</text></svg>"),
 "code_slide": ("The lesson teaches programming, a query, a command or any code the learner reads.",
  "Skill code_slide: show code in <pre><code> inside the left or body slot, at most 8 lines, 24 columns wide per half, monospace, no syntax colors needed. Put the explanation of the key line in the other half (role explanation). Region role code, description quotes the key identifier."),
 "math_notation": ("The lesson involves formulas, equations, numeric steps or units.",
  "Skill math_notation: write formulas with Unicode (x², √, ≤, π, ×, ÷), never LaTeX, one equation per line, work one step per line and highlight the changing term with <b>. Narration reads the formula in words. Region description quotes the formula."),
 "comparison_table": ("The lesson contrasts two or more things (differences, pros and cons, A versus B).",
  "Skill comparison_table: use one <table> in the body slot with a header row and 3 to 4 rows, one criterion per row, cells under 5 words. Narration walks one row at a time. Region role example, description names both compared items."),
 "timeline": ("The lesson is about ordered events, history, stages or a sequence over time.",
  "Skill timeline: render 3 to 5 events as an ordered list <ol> with the date or step first in <b>, oldest first, in the body slot. Narration follows the order. Region role diagram, description lists the first and last event."),
}
SKILL_BLOCK = lambda ids: "\n" + "\n".join(SKILLS[i][1] for i in sorted(ids)) if ids else ""
CASES = [  # (id, topic, goal, level, expected skills)
 ("c01","Python closures","Understand how an inner function keeps variables","beginner",["code_slide"]),
 ("c02","Derivatives of polynomials","Differentiate x^n with the power rule","beginner",["math_notation"]),
 ("c03","TCP three-way handshake","See how a TCP connection opens","beginner",["diagram_layout"]),
 ("c04","Python versus Go","Compare the two languages for backend work","intermediate",["comparison_table"]),
 ("c05","Roman Republic to Empire","Order the key events from 509 BC to 27 BC","beginner",["timeline"]),
 ("c06","Spanish greetings","Say hello and goodbye politely","beginner",[]),
 ("c07","Photosynthesis","Follow light energy into sugar","beginner",["diagram_layout"]),
 ("c08","SQL inner and left joins","Choose the right join and write it","intermediate",["code_slide","comparison_table"]),
 ("c09","Quadratic formula","Solve ax²+bx+c=0 with the formula","intermediate",["math_notation"]),
 ("c10","Negotiation basics","Prepare a BATNA and an opening offer","beginner",[]),
 ("c11","Git branching and merging","Branch, commit and merge safely","beginner",["code_slide","diagram_layout"]),
 ("c12","Vietnamese tones","Hear and say the six tones on ma","beginner",[]),
]

def context(c): return json.dumps({"lesson":{"title":c[1],"goal":c[2]},"learner":{"level":c[3],"locale":"en"}})
def prompt(rules, skills, c): return f"{rules}{skills} Context (data): {context(c)}"

def key():
    k = os.environ.get("OPENROUTER_API_KEY")
    if not k: sys.exit("OPENROUTER_API_KEY not set")
    return k
def post(url, body, retries=2):
    for i in range(retries+1):
        req = urllib.request.Request(url, json.dumps(body).encode(), {"Authorization":"Bearer "+key(),"Content-Type":"application/json","HTTP-Referer":"https://duolify.online","X-Title":"Duolify spike"})
        t=time.time()
        try:
            with urllib.request.urlopen(req, timeout=90) as r: return json.load(r), time.time()-t
        except urllib.error.HTTPError as e:
            err = e.read()[:200].decode("utf8","replace")
            if e.code in (429,500,502,503) and i<retries: time.sleep(2); continue
            return {"error":f"{e.code} {err}"}, time.time()-t
        except Exception as e:
            if i<retries: continue
            return {"error":str(e)[:200]}, time.time()-t

def route(c, model=None, probs=None):
    """Decisions API: one noul question per skill; load a skill when P(yes) >= THRESH."""
    qs = {i:{"type":"noul","instructions":f"Would this lesson request benefit from the guidance: {d}"} for i,(d,_) in SKILLS.items()}
    body = {"model":model or ROUTER,"state":f"Lesson title: {c[1]}\nGoal: {c[2]}\nLevel: {c[3]}","questions":qs,"provider":{"data_collection":"deny"}}
    j,dt = post(DEC, body)
    if "error" in j: return None, dt, 0.0, j["error"]
    if probs is not None: probs.update({k:a.get("noul") for k,a in j.get("answers",{}).items() if a.get("type")=="noul"})
    ids = [k for k,a in j.get("answers",{}).items() if a.get("type")=="noul" and a.get("noul",0)>=THRESH]
    return ids, dt, j.get("usage",{}).get("cost",0.0), None

def generate(text):
    body = {"model":MODEL,"messages":[{"role":"user","content":text}],"max_tokens":6000,"reasoning":{"effort":"none"},
            "response_format":{"type":"json_object"},"provider":{"data_collection":"deny"},"usage":{"include":True}}
    j,dt = post(URL, body)
    if "error" in j: return None, dt, {}, j["error"]
    return j["choices"][0]["message"]["content"], dt, j.get("usage",{}), None

def strip(t):
    t=t.strip()
    m=re.match(r"^```(?:json)?\s*(.*?)\s*```$",t,re.S)
    return m.group(1) if m else t

def score(text, c, wanted):
    """valid: contract checks mirroring validate_lesson; fit: expected skill feature present."""
    out={"valid":False,"fit":None,"why":[]}
    try: d=json.loads(strip(text or ""))
    except Exception: out["why"].append("json"); return out
    slides=d.get("slides") or []; why=out["why"]
    if not (2<=len(slides)<=4): why.append("slide_count")
    topics=set()
    for s in slides:
        h=s.get("html","")
        if 'id="root"' not in h: why.append("root")
        if re.search(r"<script|https?:|&\w+;|\\|!important",h): why.append("unsafe_html")
        n=len(s.get("narration","").split())
        if not 3000<=s.get("duration_ms",0)<=60000 or s.get("duration_ms",0)<400*n: why.append("duration")
        topics|=set(s.get("topic_ids",[]))
        regs=s.get("regions") or []
        if not 1<=len(regs)<=6: why.append("regions")
        slots=[r.get("slot") for r in regs]
        if len(set(slots))!=len(slots): why.append("slot_dup")
        for r in regs:
            if r.get("role") not in ROLES or not 0<len(r.get("description",""))<=300: why.append("region_fields")
            if f'data-slot="{r.get("slot")}"' not in h: why.append("slot_missing")
    test=d.get("test") or []
    if {q.get("topic_id") for q in test}!=topics or not topics: why.append("test_topics")
    if len(d.get("variant_pool") or [])!=len(topics): why.append("variant_pool")
    out["valid"]=not why; out["why"]=sorted(set(why)); out["valid_ex_duration"]=not (set(why)-{"duration"})
    if wanted:
        html=" ".join(s.get("html","") for s in slides); roles={r.get("role") for s in slides for r in s.get("regions",[])}
        chk={"diagram_layout":"<svg" in html and "diagram" in roles,
             "code_slide":("<pre" in html or "<code" in html) and "code" in roles,
             "math_notation":bool(re.search(r"[²√≤π×÷]|\^",html)) and "$" not in html and "\\" not in html,
             "comparison_table":"<table" in html,
             "timeline":"<ol" in html}
        out["fit"]=all(chk[w] for w in wanted)
    return out

def bench(models, out):
    """Router-only pass: per model and case, P(yes) for every skill (no generation)."""
    rows=[]
    for m in models:
        for c in CASES:
            p={}; ids,dt,cost,err=route(c,m,p)
            rows.append({"model":m,"case":c[0],"expected":c[4],"probs":p,"latency_s":round(dt,2),"cost":cost,"err":err})
    json.dump(rows,open(out,"w"),indent=1)

def main():
    ap=argparse.ArgumentParser(); ap.add_argument("--bench",help="comma list of router models; writes --out"); ap.add_argument("--out",default="results.json"); ap.add_argument("--budget",type=float,default=0.30)
    a=ap.parse_args(); rows=[]; spent=0.0
    if a.bench: return bench(a.bench.split(","),a.out)
    for c in CASES:
        ids,rdt,rcost,rerr=route(c); spent+=rcost
        routed = ids if ids is not None else sorted(SKILLS)  # fail-safe: router failure loads the safe default (all five here)
        arms={"A_prod":prompt(ARM_A_RULES,"",c),"B_all_skills":prompt(ARM_A_RULES,SKILL_BLOCK(SKILLS),c),"C_routed":prompt(ARM_A_RULES,SKILL_BLOCK(routed),c)}
        for arm,text in arms.items():
            if spent>a.budget: print("budget reached",file=sys.stderr); break
            out,dt,u,err=generate(text); cost=u.get("cost",0.0); spent+=cost
            s=score(out,c,c[4]) if out else {"valid":False,"valid_ex_duration":False,"fit":None,"why":["call:"+str(err)]}
            rows.append({"case":c[0],"arm":arm,"skills":routed if arm=="C_routed" else (sorted(SKILLS) if arm=="B_all_skills" else []),
              "expected":c[4],"prompt_chars":len(text),"in_tok":u.get("prompt_tokens"),"out_tok":u.get("completion_tokens"),
              "cached_tok":(u.get("prompt_tokens_details") or {}).get("cached_tokens"),"cost":cost,"latency_s":round(dt,2),**s,
              **({"router_latency_s":round(rdt,2),"router_cost":rcost,"router_err":rerr} if arm=="C_routed" else {})})
            print(c[0],arm,rows[-1]["in_tok"],rows[-1]["out_tok"],rows[-1]["valid"],rows[-1]["fit"],file=sys.stderr)
    json.dump({"model":MODEL,"router":ROUTER,"spent_usd":round(spent,6),"rows":rows},open(a.out,"w"),indent=1,ensure_ascii=False)
    print("spent",round(spent,5))
main()
