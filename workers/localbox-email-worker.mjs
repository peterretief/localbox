export default {
  async email(message, env) {
    const recipient = message.to;
    const sender = message.from;
    const raw = await new Response(message.raw).text();
    const route = getRoute(recipient, env);

    for (const target of route.targets) {
      const result = await deliver(target, { to: recipient, from: sender, raw });
      if (result.ok) {
        console.log("[OK] Delivered " + recipient + " to " + target.name);
        return;
      }
      console.log("[FAIL] " + target.name + " for " + recipient + ": " + result.error);
    }

    message.setReject("LocalBox ingest unavailable");
  }
};

function getRoute(recipient, env) {
  const domain = String(recipient || "").toLowerCase().split("@").pop();
  const routes = parseRouteMap(env.LOCALBOX_ROUTE_MAP);
  const configured = routes[domain] || routes["*"];
  if (configured) {
    const targets = [];
    if (configured.home?.url) {
      targets.push({ name: domain + ":home", url: configured.home.url, secret: env.LOCALBOX_INGEST_SECRET });
    }
    if (configured.backup?.url) {
      targets.push({ name: domain + ":backup", url: configured.backup.url, secret: env.LOCALBOX_BACKUP_INGEST_SECRET });
    }
    if (targets.length > 0) {
      return { targets };
    }
  }

  return {
    targets: compactTargets([{
      name: domain + ":default",
      url: env.LOCALBOX_INGEST_URL,
      secret: env.LOCALBOX_INGEST_SECRET
    }, {
      name: domain + ":backup",
      url: env.LOCALBOX_BACKUP_INGEST_URL,
      secret: env.LOCALBOX_BACKUP_INGEST_SECRET
    }])
  };
}

function compactTargets(targets) {
  return targets.filter((target) => target.url && target.secret);
}

function parseRouteMap(value) {
  if (!value) {
    return {};
  }
  try {
    return JSON.parse(value);
  } catch (err) {
    console.log("[CONFIG] Invalid LOCALBOX_ROUTE_MAP: " + err.message);
    return {};
  }
}

async function deliver(target, payload) {
  if (!target.url || !target.secret) {
    return { ok: false, error: "missing url or secret" };
  }
  try {
    const response = await fetch(target.url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Ingest-Secret": target.secret
      },
      body: JSON.stringify(payload)
    });

    if (response.ok) {
      return { ok: true };
    }

    const text = await response.text().catch(() => "");
    return { ok: false, error: "HTTP " + response.status + " " + text };
  } catch (err) {
    return { ok: false, error: err.message };
  }
}
