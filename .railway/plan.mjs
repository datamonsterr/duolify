#!/usr/bin/env node
import * as moduleApi from "node:module";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const stubPath = path.join(__dirname, "stub-iac.mjs");
const stubUrl = pathToFileURL(stubPath).href;

// Intercept imports of "railway/iac" with local offline stub
if (typeof moduleApi.registerHooks === "function") {
  moduleApi.registerHooks({
    resolve(specifier, context, nextResolve) {
      if (specifier === "railway/iac") {
        return { format: "module", shortCircuit: true, url: stubUrl };
      }
      return nextResolve(specifier, context);
    },
  });
} else if (typeof moduleApi.register === "function") {
  const loaderUrl =
    "data:text/javascript," +
    encodeURIComponent(`
    export function resolve(specifier, context, nextResolve) {
      if (specifier === "railway/iac") {
        return { format: "module", shortCircuit: true, url: ${JSON.stringify(stubUrl)} };
      }
      return nextResolve(specifier, context);
    }
  `);
  moduleApi.register(loaderUrl, import.meta.url);
}

// Load git-backed railway.ts
const railwayPath = path.join(__dirname, "railway.ts");
const railwayMod = await import(pathToFileURL(railwayPath).href);
const defineRailwayFn = railwayMod.default || railwayMod;

function renderEnvironment(env) {
  const project = defineRailwayFn({ projectName: "duolify", environment: env });
  const services = [];
  const resources = [];

  for (const res of project.resources || []) {
    if (res.type === "service") {
      // Resolve env proxy to a plain object
      const envObj = {};
      if (res.env) {
        for (const [k, v] of Object.entries(res.env)) {
          envObj[k] = typeof v === "object" && v !== null ? JSON.stringify(v) : String(v);
        }
      }
      services.push({
        name: res.name,
        start: res.start || (res.deploy && res.deploy.startCommand) || "",
        preDeploy:
          res.preDeploy ||
          (res.deploy && res.deploy.preDeployCommand && res.deploy.preDeployCommand[0]) ||
          "",
        healthcheck:
          res.healthcheck || (res.deploy && res.deploy.healthcheckPath) || "",
        healthcheckTimeout:
          res.healthcheckTimeout ||
          (res.deploy && res.deploy.healthcheckTimeout) ||
          0,
        domains: res.domains || [],
        env: envObj,
      });
    } else {
      resources.push({
        name: res.name,
        type: res.kind || res.type,
        region: res.config?.region || "",
      });
    }
  }

  return {
    environment: env,
    projectName: project.projectName || project.name || "duolify",
    services,
    resources,
  };
}

function parseArgs() {
  const args = process.argv.slice(2);
  let env = null;
  let all = false;

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === "--all") {
      all = true;
    } else if (arg === "--env" || arg === "--environment") {
      env = args[++i];
    } else if (!arg.startsWith("-")) {
      env = arg;
    }
  }

  return { env, all };
}

try {
  const { env, all } = parseArgs();
  if (all || (!env && !all)) {
    // Render all environments: dev, staging, prod
    const plans = ["dev", "staging", "prod"].map((e) => renderEnvironment(e));
    process.stdout.write(JSON.stringify(plans, null, 2) + "\n");
  } else {
    if (!["dev", "staging", "prod"].includes(env)) {
      console.error(`Invalid environment: ${env}. Must be dev, staging, or prod.`);
      process.exit(1);
    }
    const plan = renderEnvironment(env);
    process.stdout.write(JSON.stringify(plan, null, 2) + "\n");
  }
} catch (err) {
  console.error("Plan rendering failed:", err.message);
  process.exit(1);
}
