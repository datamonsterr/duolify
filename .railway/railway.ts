import {
  bucket, defineRailway, github, postgres, preserve, project, redis, ref, service,
} from "railway/iac";

export default defineRailway((ctx) => {
  if (ctx.environment !== "dev" || ctx.projectName !== "duolify") {
    throw new Error("Bootstrap IaC is restricted to project duolify / environment dev");
  }

  const db = postgres("postgres");
  const cache = redis("redis");
  const assets = bucket("lesson-assets", { region: "sin" });
  const backups = bucket("backups", { region: "sin" });
  const engine = service("engine", {
    source: github("datamonsterr/duolify", { branch: "main" }),
    build: { builder: "DOCKERFILE", dockerfilePath: "engine/Dockerfile" },
    start: "/duolify-engine",
    // Railway healthchecks require HTTP; gRPC health is probed in Compose.
    domains: [],
    env: {
      ENGINE_ADDR_LISTEN: "[::]:50051",
      ENGINE_PROVIDER_MODE: "mock",
      ANTHROPIC_API_KEY: preserve(),
      OPENROUTER_API_KEY: preserve(),
    },
  });

  const runtime = {
    DUOLIFY_ENV: "development",
    LOG_LEVEL: "info",
    API_ADDR: "[::]:8080",
    PORT: "8080",
    DATABASE_URL: db.env.DATABASE_URL,
    REDIS_URL: cache.env.REDIS_URL,
    ENGINE_HOST: engine.env.RAILWAY_PRIVATE_DOMAIN,
    ENGINE_ADDR: "http://${{ENGINE_HOST}}:50051",
    S3_ENDPOINT: ref(assets, "ENDPOINT"),
    S3_BUCKET: ref(assets, "BUCKET"),
    S3_REGION: ref(assets, "REGION"),
    S3_ACCESS_KEY_ID: ref(assets, "ACCESS_KEY_ID"),
    S3_SECRET_ACCESS_KEY: ref(assets, "SECRET_ACCESS_KEY"),
    BACKUP_S3_ENDPOINT: ref(backups, "ENDPOINT"),
    BACKUP_S3_BUCKET: ref(backups, "BUCKET"),
    BACKUP_S3_REGION: ref(backups, "REGION"),
    // Secret-managed scoped get/conditional-put writer, without delete/admin.
    // Never substitute the bucket's administrative credentials here.
    BACKUP_S3_ACCESS_KEY_ID: preserve(),
    BACKUP_S3_SECRET_ACCESS_KEY: preserve(),
    JWT_SECRET: preserve(),
    POSTHOG_API_KEY: preserve(),
    POSTHOG_HOST: "https://us.i.posthog.com",
  };
  const source = github("datamonsterr/duolify", {
    branch: "main", rootDirectory: "/services/api",
  });
  const build = { builder: "DOCKERFILE" as const, dockerfilePath: "Dockerfile" };
  const api = service("api", {
    source, build,
    start: "/api serve",
    preDeploy: "/api migrate",
    healthcheck: "/readyz",
    healthcheckTimeout: 120,
    env: runtime,
  });
  const worker = service("worker", {
    source, build,
    // Idempotent migrations also avoid relying on cross-service deploy order.
    preDeploy: "/api migrate",
    start: "/api worker",
    env: { ...runtime, BACKUP_AGE_RECIPIENT: preserve() },
  });
  return project("duolify", { resources: [db, cache, assets, backups, api, worker, engine] });
});
