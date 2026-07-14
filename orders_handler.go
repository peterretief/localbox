package main

import (
	"html/template"
	"net/http"
)

func ordersHandler(w http.ResponseWriter, r *http.Request) {
	if s := requireBoxAdmin(w, r); s == nil {
		return
	}

	selectedID := r.URL.Query().Get("id")
	filter := ordersFilterFromRequest(r)
	data := ordersViewFiltered(selectedID, filter)
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			data.Error = "Invalid form"
		} else {
			selectedID = r.FormValue("order_id")
			filter = ordersFilterFromRequest(r)
			var err error
			switch r.FormValue("action") {
			case "save_quote":
				err = saveOrderQuote(selectedID, r.FormValue("quoted_amount"), r.FormValue("payment_url"))
				data.Success = "Quote and payment link saved."
			case "mark_paid":
				err = markOrderPaid(selectedID, r.FormValue("payment_reference"))
				data.Success = "Order marked paid."
			case "provision_domain":
				provisioner, provisionErr := newRealDomainProvisioner()
				if provisionErr != nil {
					err = provisionErr
				} else {
					err = provisionPaidDomainOrderWith(r.Context(), selectedID, provisioner)
				}
				data.Success = "Domain registered and Cloudflare routing created."
			case "ready_for_registration":
				err = markOrderReadyForRegistration(selectedID, r.FormValue("registrar_reference"))
				data.Success = "Order marked ready for registration."
			case "complete_registration":
				err = completeOrderRegistration(r.Context(), selectedID, r.FormValue("registrar_reference"))
				data.Success = "Domain registration completed and certificate email sent."
			case "domain_propagated":
				err = markOrderDomainPropagated(r.Context(), selectedID)
				data.Success = "Domain propagation confirmed and website link sent."
			case "archive_order":
				err = archiveOrder(selectedID)
				data.Success = "Order archived."
			case "restore_order":
				err = restoreOrder(selectedID)
				data.Success = "Order restored."
			default:
				err = errUnknownAdminAction()
			}
			if err != nil {
				data.Error = err.Error()
				data.Success = ""
			}
			fresh := ordersViewFiltered(selectedID, filter)
			data.Orders = fresh.Orders
			data.DNSPending = fresh.DNSPending
			data.Current = fresh.Current
			data.Query = fresh.Query
			data.ShowProcessed = fresh.ShowProcessed
			data.ShowArchived = fresh.ShowArchived
			if data.Error == "" && fresh.Error != "" {
				data.Error = fresh.Error
			}
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl := template.Must(template.New("orders.html").Funcs(template.FuncMap{
		"formatRand": formatRand,
	}).ParseFiles("templates/orders.html"))
	tmpl.Execute(w, data)
}

func ordersFilterFromRequest(r *http.Request) OrdersFilter {
	return OrdersFilter{
		Query:         r.FormValue("q"),
		ShowProcessed: r.FormValue("show_processed") == "1",
		ShowArchived:  r.FormValue("show_archived") == "1",
	}
}
