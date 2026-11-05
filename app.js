import { supabase } from "./auth.js";

(function () {
  "use strict";

  var base = window.API_BASE_URL || "";
  var form = document.getElementById("order-form");
  var lookupForm = document.getElementById("lookup-form");
  var payment = document.getElementById("payment");
  var authPanel = document.getElementById("auth-panel");
  var authForm = document.getElementById("auth-form");
  var currentUser = null;
  var qty = 1;
  var prices = { male: 2000, female: 1000 };

  function $(id) { return document.getElementById(id); }
  function money(value) { return "₦" + Number(value).toLocaleString("en-NG"); }
  function selected() { return document.querySelector("input[name=ticket_type]:checked").value; }
  function message(text) { $("error").textContent = text; }
  function lookupMessage(text) { $("lookup-error").textContent = text; }
  function saveReference(reference) { try { localStorage.setItem("vf3_order_reference", reference); } catch (error) {} }
  function clearReference() { try { localStorage.removeItem("vf3_order_reference"); } catch (error) {} }
  function authMessage(text) { $("auth-error").textContent = text; }
  function unlock(user) {
    currentUser = user;
    authPanel.hidden = true;
    form.hidden = false;
    $("email").value = user.email || "";
    $("email").readOnly = true;
    var name = user.user_metadata && (user.user_metadata.full_name || user.user_metadata.name);
    if (name && !$('name').value) $("name").value = name;
  }
  async function initAuth() {
    if (!supabase) { authMessage("Supabase anon key is missing. Set VITE_SUPABASE_ANON_KEY in .env and restart Vite."); return; }
    var sessionResult = await supabase.auth.getSession();
    if (sessionResult.data.session) unlock(sessionResult.data.session.user);
    supabase.auth.onAuthStateChange(function (_event, session) { if (session) unlock(session.user); });
  }
  function update() {
    var type = selected();
    $("both-quantities").hidden = type !== "both";
    if (type === "both") {
      var male = Number($("male-quantity").value) || 0;
      var female = Number($("female-quantity").value) || 0;
      $("quantity").textContent = male + female;
      $("total").textContent = money(male * 2000 + female * 1000);
      return;
    }
    $("quantity").textContent = qty;
    $("total").textContent = money(prices[type] * qty);
  }
  function copyButtons() {
    payment.querySelectorAll("[data-copy]").forEach(function (button) {
      button.onclick = async function () {
        try {
          await navigator.clipboard.writeText(button.dataset.copy);
          button.textContent = "✓";
          setTimeout(function () { button.textContent = "⧉"; }, 1400);
        } catch (error) {
          button.textContent = "!";
        }
      };
    });
  }
  function hideLookup() {
    if (lookupForm) lookupForm.closest(".lookup-panel").hidden = true;
  }
  function showOrder(order) {
    saveReference(order.reference);
    if (order.status === "paid") renderTickets(order); else renderOrder(order);
  }
  function renderOrder(order) {
    form.hidden = true;
    hideLookup();
    payment.hidden = false;
    payment.innerHTML = '<div class="pay"><p><strong>Order reference:</strong> ' + order.reference + ' <button class="copy-button" type="button" aria-label="Copy order reference" title="Copy order reference" data-copy="' + order.reference + '">⧉</button></p><p>Pay <strong>' + money(order.total) + '</strong> to:</p><p><strong>Account number</strong> <button class="copy-button" type="button" aria-label="Copy account number" title="Copy account number" data-copy="8075340904">⧉</button><br><code>8075340904</code><br><strong>Bank</strong> <button class="copy-button" type="button" aria-label="Copy bank name" title="Copy bank name" data-copy="Moniepoint">⧉</button><br>Moniepoint<br><strong>Account name</strong> <button class="copy-button" type="button" aria-label="Copy account name" title="Copy account name" data-copy="Faruq Olabanji Adigun">⧉</button><br>Faruq Olabanji Adigun</p><p>Payment is awaiting admin confirmation. Keep this page or use Find my order after a refresh.</p><button class="cta" id="refresh" type="button">Check payment status</button></div>';
    copyButtons();
    $("refresh").onclick = function () { loadOrder(order.reference); };
  }
  function renderTickets(order) {
    form.hidden = true;
    hideLookup();
    payment.hidden = false;
    payment.innerHTML = '<div class="pay"><strong>Payment confirmed.</strong><p>Reference: ' + order.reference + '</p></div>' + order.tickets.map(function (ticket) {
      return '<article class="ticket"><header><strong>Vibez and Fun Party 3.0</strong><span>PAID</span></header><div class="body"><div><p class="code">' + ticket.code + '</p><p>' + order.name + '</p><p>' + order.ticket_type + ' ticket</p><p>26 December 2026, 9pm</p><p>Aiyeyemi Palace Hotel, Ajase Ipo</p><a class="ghost" href="data:image/png;base64,' + ticket.qr_png + '" download="' + ticket.code + '.png">Download ticket</a></div><img src="data:image/png;base64,' + ticket.qr_png + '" alt="QR code for ticket ' + ticket.code + '"></div><footer>Show this QR code at the door.</footer></article>';
    }).join("") + '<button class="ghost" id="again" type="button">Book another ticket</button>';
    $("again").onclick = function () { clearReference(); location.reload(); };
  }
  async function loadOrder(reference) {
    try {
      var response = await fetch(base + "/api/orders/" + encodeURIComponent(reference));
      var data = await response.json();
      if (!response.ok) throw Error(data.error || "Could not load order");
      showOrder(data);
    } catch (error) { message(error.message); }
  }
  lookupForm.addEventListener("submit", async function (event) {
    event.preventDefault();
    lookupMessage("");
    try {
      var response = await fetch(base + "/api/orders/find", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email: $("lookup-email").value.trim(), phone: $("lookup-phone").value.trim() }) });
      var data = await response.json();
      if (!response.ok) throw Error(data.error || "Could not find order");
      showOrder(data);
    } catch (error) { lookupMessage(error.message); }
  });
  authForm.addEventListener("submit", async function (event) {
    event.preventDefault();
    authMessage("");
    if (!supabase) { authMessage("Supabase anon key is missing. Set VITE_SUPABASE_ANON_KEY in .env and restart Vite."); return; }
    var result = await supabase.auth.signInWithPassword({ email: $("auth-email").value.trim(), password: $("auth-password").value });
    if (result.error) authMessage(result.error.message); else unlock(result.data.user);
  });
  $("signup").onclick = async function () {
    authMessage("");
    if (!supabase) { authMessage("Supabase anon key is missing. Set VITE_SUPABASE_ANON_KEY in .env and restart Vite."); return; }
    var result = await supabase.auth.signUp({ email: $("auth-email").value.trim(), password: $("auth-password").value });
    if (result.error) authMessage(result.error.message); else authMessage("Check your email to confirm your account, then sign in.");
  };
  document.querySelectorAll('input[name="ticket_type"]').forEach(function (input) { input.addEventListener("change", update); });
  $("minus").onclick = function () { if (qty > 1) { qty--; update(); } };
  $("plus").onclick = function () { if (qty < 10) { qty++; update(); } };
  form.addEventListener("submit", async function (event) {
    event.preventDefault();
    message("");
    var type = selected();
    var maleQuantity = type === "both" ? Number($("male-quantity").value) || 0 : type === "male" ? qty : 0;
    var femaleQuantity = type === "both" ? Number($("female-quantity").value) || 0 : type === "female" ? qty : 0;
    var payload = { ticket_type: type, quantity: qty, male_quantity: maleQuantity, female_quantity: femaleQuantity, name: $("name").value.trim(), phone: $("phone").value.trim(), email: $("email").value.trim() };
    if (maleQuantity + femaleQuantity < 1 || maleQuantity + femaleQuantity > 10) { message("Choose between 1 and 10 tickets."); return; }
    if (payload.name.length < 2 || !/^\+?(234|0)\d{10}$/.test(payload.phone.replace(/[\s-]/g, "")) || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(payload.email)) { message("Enter a valid name, Nigerian phone number, and email."); return; }
    var button = form.querySelector("button[type=submit]");
    button.disabled = true;
    try {
      var response = await fetch(base + "/api/orders", { method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(payload) });
      var data = await response.json();
      if (!response.ok) throw Error(data.error || "Could not create order");
      showOrder(data);
    } catch (error) { message(error.message); button.disabled = false; }
  });
  $("male-quantity").addEventListener("input", update);
  $("female-quantity").addEventListener("input", update);
  update();
  initAuth();
  try { var saved = localStorage.getItem("vf3_order_reference"); if (saved) loadOrder(saved); } catch (error) {}
})();
