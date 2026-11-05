VITE_SUPABASE_ANON_KEY=https://vilwohwxdwwuetidggkl.supabase.co/rest/v1/
  var base = window.API_BASE_URL || "";
  var $ = function (id) { return document.getElementById(id); };

  function money(value) {
    return "₦" + Number(value).toLocaleString("en-NG");
  }

  function escapeHtml(value) {
    return String(value == null ? "" : value).replace(/[&<>'"]/g, function (char) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[char];
    });
  }

  async function api(path, options) {
    var response = await fetch(base + path, options);
    var data = await response.json().catch(function () { return {}; });
    if (!response.ok) throw Error(data.error || "Request failed");
    return data;
  }

  function renderDashboard(data) {
    $("login").hidden = true;
    $("dashboard").hidden = false;
    $("paid").textContent = data.paid_orders;
    $("pending").textContent = data.pending_orders;
    $("revenue").textContent = money(data.revenue);
    $("orders").innerHTML = data.orders.map(function (order) {
      var action = order.status === "pending"
        ? '<button class="ghost mark-paid" data-ref="' + escapeHtml(order.reference) + '">Mark paid</button>'
        : "";
      return '<article class="list">' +
        '<strong>' + escapeHtml(order.reference) + '</strong> · ' + escapeHtml(order.status) +
        '<p><strong>Customer:</strong> ' + escapeHtml(order.name) + '</p>' +
        '<p><strong>Phone:</strong> <a href="tel:' + escapeHtml(order.phone) + '">' + escapeHtml(order.phone) + '</a></p>' +
        '<p><strong>Tickets:</strong> ' + escapeHtml(order.ticket_type) + ' × ' + escapeHtml(order.quantity) + ' · ' + money(order.total) + '</p>' +
        action +
        '</article>';
    }).join("");

    document.querySelectorAll(".mark-paid").forEach(function (button) {
      button.onclick = async function () {
        if (!window.confirm("Confirm that this payment has been received before issuing the ticket?")) return;
        button.disabled = true;
        button.textContent = "Confirming...";
        try {
          await api("/api/admin/orders/" + encodeURIComponent(button.dataset.ref) + "/mark-paid", { method: "POST" });
          await loadDashboard();
        } catch (error) {
          button.disabled = false;
          button.textContent = "Mark paid";
          window.alert(error.message);
        }
      };
    });
  }

  async function loadDashboard() {
    try {
      renderDashboard(await api("/api/admin/dashboard"));
    } catch (error) {
      $("login").hidden = false;
    }
  }

  $("login-form").onsubmit = async function (event) {
    event.preventDefault();
    $("login-error").textContent = "";
    try {
      await api("/api/admin/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ Username: $("username").value, Password: $("password").value })
      });
      await loadDashboard();
    } catch (error) {
      $("login-error").textContent = error.message;
    }
  };

  $("logout").onclick = async function () {
    await api("/api/admin/logout", { method: "POST" });
    location.reload();
  };

  loadDashboard();
})();
