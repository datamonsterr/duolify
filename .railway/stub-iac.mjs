// Stub implementation of railway/iac module for offline plan rendering without network or credentials.

export function defineRailway(fn) {
  return fn;
}

export function postgres(name, config = {}) {
  return {
    address: `database.${name}`,
    type: "database",
    kind: "postgres",
    name,
    config,
    env: {
      DATABASE_URL: `\${{database.${name}.DATABASE_URL}}`,
    },
  };
}

export function redis(name, config = {}) {
  return {
    address: `database.${name}`,
    type: "database",
    kind: "redis",
    name,
    config,
    env: {
      REDIS_URL: `\${{database.${name}.REDIS_URL}}`,
    },
  };
}

export function bucket(name, config = {}) {
  return {
    address: `bucket.${name}`,
    type: "bucket",
    name,
    config,
    env: new Proxy({}, {
      get: (_, prop) => `\${{bucket.${name}.${String(prop)}}}`,
    }),
  };
}

export function service(name, config = {}) {
  const envMap = { ...(config.env || {}) };
  return {
    address: `service.${name}`,
    type: "service",
    name,
    ...config,
    domains: config.domains || [],
    env: new Proxy(envMap, {
      get: (target, prop) => {
        if (prop in target) return target[prop];
        return `\${{service.${name}.${String(prop)}}}`;
      },
    }),
  };
}

export function github(repo, config = {}) {
  return {
    type: "github",
    repo,
    ...config,
  };
}

export function preserve() {
  return "preserve()";
}

export function ref(resource, output) {
  const target = resource.address || resource.name;
  return `\${{${target}.${output}}}`;
}

export function project(name, config = {}) {
  return {
    name,
    projectName: name,
    ...config,
  };
}
