const profilesEl = document.querySelector("#profiles");
const notice = document.querySelector("#notice");
const credentialsCard = document.querySelector("#credentials-card");
const credentialName = document.querySelector("#credential-name");

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {"Content-Type": "application/json", ...(options.headers || {})}
  });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || "Request failed");
  return body;
}

function message(text, error = false) {
  notice.textContent = text;
  notice.className = error ? "error" : "success";
}

async function loadProfiles() {
  try {
    const profiles = await api("/api/profiles");
    profilesEl.replaceChildren();
    if (!profiles.length) {
      profilesEl.innerHTML = '<p class="empty">No profiles yet. Import your first .ovpn file below.</p>';
      return;
    }
    for (const profile of profiles) {
      const row = document.createElement("article");
      row.className = "profile";
      const info = document.createElement("div");
      const title = document.createElement("strong");
      title.textContent = profile.name;
      const status = document.createElement("span");
      status.className = `status ${profile.status}`;
      status.textContent = profile.status;
      info.append(title, status);
      const actions = document.createElement("div");
      actions.className = "actions";
      actions.append(
        button("Credentials", () => showCredentials(profile.name)),
        button(profile.status === "disconnected" ? "Connect" : "Disconnect",
          () => action(profile.name, profile.status === "disconnected" ? "connect" : "disconnect"), true)
      );
      row.append(info, actions);
      profilesEl.append(row);
    }
  } catch (error) { message(error.message, true); }
}

function button(label, handler, primary = false) {
  const element = document.createElement("button");
  element.textContent = label;
  if (primary) element.className = "primary";
  element.addEventListener("click", handler);
  return element;
}

function showCredentials(name) {
  credentialName.textContent = name;
  credentialsCard.dataset.profile = name;
  credentialsCard.classList.remove("hidden");
  credentialsCard.scrollIntoView({behavior: "smooth"});
}

async function action(name, actionName) {
  try {
    await api(`/api/profiles/${encodeURIComponent(name)}/${actionName}`, {method: "POST"});
    message(`${name}: ${actionName} requested.`);
    setTimeout(loadProfiles, 700);
  } catch (error) { message(error.message, true); }
}

document.querySelector("#refresh").addEventListener("click", loadProfiles);
document.querySelector("#import-form").addEventListener("submit", async event => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(event.currentTarget));
  try {
    await api("/api/profiles", {method: "POST", body: JSON.stringify(data)});
    event.currentTarget.reset();
    message(`Profile ${data.name} imported.`);
    await loadProfiles();
    showCredentials(data.name.toLowerCase());
  } catch (error) { message(error.message, true); }
});
document.querySelector("#credentials-form").addEventListener("submit", async event => {
  event.preventDefault();
  const name = credentialsCard.dataset.profile;
  const data = Object.fromEntries(new FormData(event.currentTarget));
  try {
    await api(`/api/profiles/${encodeURIComponent(name)}/credentials`, {
      method: "POST", body: JSON.stringify(data)
    });
    event.currentTarget.reset();
    credentialsCard.classList.add("hidden");
    message(`Credentials for ${name} encrypted on this Windows account.`);
  } catch (error) { message(error.message, true); }
});
document.querySelector("#quit").addEventListener("click", async () => {
  try {
    await api("/api/quit", {method: "POST", body: "{}"});
    document.body.replaceChildren("TunnelDesk has closed. You may close this tab.");
  } catch (error) { message(error.message, true); }
});

loadProfiles();
setInterval(loadProfiles, 5000);
