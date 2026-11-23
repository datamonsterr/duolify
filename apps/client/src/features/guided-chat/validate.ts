import type { Step, Validate, Value } from "./types";

export type Invalid = "required" | "invalid";

const hhmm = /^([01]\d|2[0-3]):[0-5]\d$/;
const iso = /^\d{4}-\d{2}-\d{2}$/;
const email = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function validateRule(rule: Validate, value: Value): Invalid | null {
  switch (rule.type) {
    case "string": {
      if (typeof value !== "string") return "invalid";
      const length = [...value.trim()].length;
      if (length < (rule.min ?? 1)) return "required";
      return length > rule.max ? "invalid" : null;
    }
    case "hhmm":
      return typeof value === "string" && hhmm.test(value) ? null : "invalid";
    case "date": {
      if (typeof value !== "string" || !iso.test(value)) return "invalid";
      const parsed = new Date(`${value}T00:00:00Z`);
      if (
        Number.isNaN(parsed.getTime()) ||
        parsed.toISOString().slice(0, 10) !== value
      )
        return "invalid";
      if (rule.min && value < rule.min) return "invalid";
      if (rule.max && value > rule.max) return "invalid";
      return null;
    }
    case "number":
      if (typeof value !== "number" || !Number.isFinite(value))
        return "invalid";
      if (rule.integer && !Number.isInteger(value)) return "invalid";
      return value < rule.min || value > rule.max ? "invalid" : null;
    case "boolean":
      return typeof value === "boolean" ? null : "invalid";
    case "rating":
      return typeof value === "number" &&
        Number.isInteger(value) &&
        value >= 1 &&
        value <= rule.max
        ? null
        : "invalid";
    case "email":
      return typeof value === "string" && email.test(value.trim())
        ? null
        : "invalid";
    case "code":
      return typeof value === "string" &&
        new RegExp(`^\\d{${rule.length}}$`).test(value.trim())
        ? null
        : "invalid";
    case "multi":
      return Array.isArray(value) && value.length <= rule.maxItems
        ? null
        : "invalid";
    case "file":
      // The widget checks size and type against the picked File; the stored value is its name.
      return typeof value === "string" && value ? null : "required";
    case "enum":
      return null; // checked against the step's options in validateStep
  }
}

export function validateStep(step: Step, value: Value): Invalid | null {
  if (step.widget === "confirm") return value === true ? null : "invalid";
  if (
    step.options &&
    (step.widget === "single" || step.widget === "chips+text")
  ) {
    const known =
      typeof value === "string" && step.options.some((o) => o.id === value);
    if (step.widget === "single") return known ? null : "invalid";
    if (known) return null;
  }
  if (step.widget === "multi") {
    const ids = step.options?.map((o) => o.id) ?? [];
    if (!Array.isArray(value) || value.some((v) => !ids.includes(v)))
      return "invalid";
  }
  if (step.widget === "toggle")
    return typeof value === "boolean" ? null : "invalid";
  if (!step.validate) return value === null || value === "" ? "required" : null;
  return validateRule(step.validate, value);
}
