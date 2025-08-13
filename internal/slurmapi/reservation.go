package slurmapi

import (
	api0043 "github.com/SlinkyProject/slurm-client/api/v0043"
)

type Reservation struct {
	Name     string
	NodeList *string
}

func ReservationFromAPI(reservation api0043.V0043ReservationInfo) Reservation {
	res := Reservation{
		Name:     *reservation.Name,
		NodeList: reservation.NodeList,
	}
	return res
}
