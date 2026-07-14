export default {
  async email(message, env) {
    const recipient = message.to;
    const sender = message.from;
    const raw = await new Response(message.raw).text();

    try {
      const response = await fetch(env.LOCALBOX_INGEST_URL, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Ingest-Secret": env.LOCALBOX_INGEST_SECRET
        },
        body: JSON.stringify({
          to: recipient,
          from: sender,
          raw
        })
      });

      if (response.ok) {
        console.log("[OK] Delivered: " + recipient);
        return;
      }

      const text = await response.text().catch(() => "");
      console.log("[REJECTED] Server said " + response.status + ": " + text);
      message.setReject("Ingester Error: " + response.status);
    } catch (err) {
      console.log("[EXCEPTION] Bridge failed: " + err.message);
      message.setReject("Worker Bridge Exception");
    }
  }
};
