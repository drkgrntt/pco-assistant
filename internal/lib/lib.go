package lib

import (
	"church/pco"
	"church/tithely"
	"log"
)

func ProcessTithelyPeopleToPCO() (processed []*tithely.Account, errors []error) {
	data, err := tithely.GetAccounts(&tithely.AccountsParams{
		Limit: 20,
	})

	if err != nil {
		log.Println("Error getting tithely accounts: ", err)
		errors = append(errors, err)
		return
	}

	notInPco := []*tithely.Account{
		// {
		// 	FirstName:   "FullTestFirst",
		// 	LastName:    "FullTestLast",
		// 	Email:       "GZDhD@example.com",
		// 	PhoneNumber: "1234567890",
		// 	CreatedDate: strconv.Itoa(int(time.Now().Unix())),
		// 	Address: tithely.Address{
		// 		StreetAddress: "123 Test St",
		// 		City:          "Test City",
		// 		State:         "Test State",
		// 		Postal:        "12345",
		// 		Country:       "US",
		// 	},
		// },
	}

	for _, account := range data.Data {
		people, err := pco.GetPeople(&pco.PeopleParams{
			FirstName: account.FirstName,
			LastName:  account.LastName,
		})

		if err != nil {
			log.Println("Error getting pco people by name: ", err, "FirstName: ", account.FirstName, "LastName: ", account.LastName)
			errors = append(errors, err)
		}

		if len(people.Data) != 0 {
			continue
		}

		people, err = pco.GetPeople(&pco.PeopleParams{
			Email: account.Email,
		})

		if err != nil {
			log.Println("Error getting pco people by email: ", err, "Email: ", account.Email)
			errors = append(errors, err)
		}

		if len(people.Data) != 0 {
			continue
		}

		people, err = pco.GetPeople(&pco.PeopleParams{
			SearchPhoneNumber: account.PhoneNumber,
		})

		if err != nil {
			log.Println("Error getting pco people by phone number: ", err, "PhoneNumber: ", account.PhoneNumber)
			errors = append(errors, err)
		}

		if len(people.Data) != 0 {
			continue
		}

		notInPco = append(notInPco, account)
	}

	for _, account := range notInPco {
		personRes, err := pco.CreatePerson(&pco.CreatePersonParams{
			FirstName: account.FirstName,
			LastName:  account.LastName,
		})
		if err != nil || personRes.Data.ID == "" {
			log.Println("Error creating PCO Person: ", err, personRes)
			if err != nil {
				errors = append(errors, err)
			}
			continue
		}

		personId := personRes.Data.ID

		emailRes, err := pco.CreateEmail(personId, &pco.EmailCreateParams{
			Address:  account.Email,
			Location: "Tithely",
		})

		if err != nil || emailRes.Data.ID == "" {
			if err != nil {
				errors = append(errors, err)
			}
			log.Println("Error creating PCO Email: ", err, emailRes)
		}

		phoneRes, err := pco.CreatePhoneNumber(personId, &pco.PhoneNumberCreateParams{
			Number:   account.PhoneNumber,
			Location: "Tithely",
		})

		if err != nil || phoneRes.Data.ID == "" {
			if err != nil {
				errors = append(errors, err)
			}
			log.Println("Error creating PCO Phone Number: ", err, phoneRes)
		}

		addressRes, err := pco.CreateAddress(personId, &pco.AddressCreateParams{
			AddressLine1: account.Address.StreetAddress,
			AddressLine2: "",
			City:         account.Address.City,
			State:        account.Address.State,
			Zip:          account.Address.Postal,
			CountryCode:  account.Address.Country,
			Location:     "Tithely",
		})

		if err != nil || addressRes.Data.ID == "" {
			if err != nil {
				errors = append(errors, err)
			}
			log.Println("Error creating PCO Address: ", err, addressRes)
		}

		processed = append(processed, account)
	}

	return
}
