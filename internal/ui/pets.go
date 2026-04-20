package ui
package ui

// Pet is a 3-row ASCII buddy with happy/sleep/alert variants.
type Pet struct {
	ID    string
	Name  string
	Happy [3]string
	Sleep [3]string
	Alert [3]string
}

// Pets is the buddy roster (Tamagotchi-style). Each pet is 3 rows tall.
var Pets = []Pet{
	{
		ID: "cat", Name: "ring0-chan",
		Happy: [3]string{` /\_/\ `, `( o.o )`, ` > ^ < ~`},
		Sleep: [3]string{` /\_/\   z`, `( -.- )  Z`, ` > _ <  z `},
		Alert: [3]string{` /\_/\ `, `( O_O )`, ` >!^!< `},
	},
	{
		ID: "chonk", Name: "chonk",
		Happy: [3]string{` /\__/\ `, `(='o'=) `, `(\"\")(\"\")`},
		Sleep: [3]string{` /\__/\  z`, `(=-.-=)  Z`, `(\"\")(\"\")  `},
		Alert: [3]string{` /\__/\ `, `(=O_O=) `, `(!!)(!!)`},
	},
	{
		ID: "dog", Name: "pupper",
		Happy: [3]string{` / \__`, `(    @\___`, ` /         O`},
		Sleep: [3]string{` / \__    z`, `(    @\_zZ`, ` /         o`},
		Alert: [3]string{` / \__!`, `(    @\__!`, ` /    GRRR O`},
	},
	{
		ID: "duck", Name: "quackers",
		Happy: [3]string{`  __    `, `<(o )___`, ` ( ._> /`},
		Sleep: [3]string{`  __  z `, `<(- )___`, ` ( ._> /`},
		Alert: [3]string{`  __ !! `, `<(O )___`, ` ( ._> / QUACK`},
	},
	{
		ID: "capy", Name: "capy",
		Happy: [3]string{`  ___      `, ` (o o)____ `, `  (________)`},
		Sleep: [3]string{`  ___    z `, ` (- -)____ `, `  (________)`},
		Alert: [3]string{`  ___ !!!  `, ` (O O)____ `, `  (________)`},
	},
	{
		ID: "dragon", Name: "smol-dragon",
		Happy: [3]string{`   /\  /\  `, `  {  ^o^  } `, `  <\__|__/> ~`},
		Sleep: [3]string{`   /\  /\  z`, `  {  -.-  }Z`, `  <\__|__/> `},
		Alert: [3]string{`   /\/\/\  `, `  { >O_O< } `, `  <\_FIRE_/>`},
	},
	{
		ID: "ghost", Name: "boo",
		Happy: [3]string{`  .-.   `, ` ( o o) `, `  \uuu/ `},
		Sleep: [3]string{`  .-.  z`, ` ( - -)Z`, `  \uuu/ `},
		Alert: [3]string{`  .-.!! `, ` ( O O) `, `  \UUU/ `},
	},
	{
		ID: "fox", Name: "vix",
		Happy: [3]string{` /\   /\ `, `(  ^.^  )`, ` \  ~  /  ~`},
		Sleep: [3]string{` /\   /\z`, `(  -.-  )Z`, ` \  _  /  `},
		Alert: [3]string{` /\   /\!`, `(  >.<  )`, ` \ !!! /  `},
	},
}

// PetByID returns the pet with the given id, or the default (first) pet.
func PetByID(id string) Pet {
	for _, p := range Pets {
		if p.ID == id {
			return p
		}
	}
	return Pets[0]
}

// NextPet returns the next pet id in the roster, wrapping around.
func NextPet(id string) string {
	for i, p := range Pets {
		if p.ID == id {
			return Pets[(i+1)%len(Pets)].ID
		}
	}
	return Pets[0].ID
}
