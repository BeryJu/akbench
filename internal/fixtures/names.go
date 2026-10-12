package fixtures

// Name lists the generator draws from - common US first and last names, in
// place of the Python script's Faker en_US provider. Collisions are fine:
// usernames get a numeric suffix (generator.uniqueUsername).

var firstNames = []string{
	"James", "Mary", "Robert", "Patricia", "John", "Jennifer", "Michael", "Linda", "David", "Elizabeth",
	"William", "Barbara", "Richard", "Susan", "Joseph", "Jessica", "Thomas", "Sarah", "Christopher", "Karen",
	"Charles", "Lisa", "Daniel", "Nancy", "Matthew", "Betty", "Anthony", "Sandra", "Mark", "Margaret",
	"Donald", "Ashley", "Steven", "Kimberly", "Andrew", "Emily", "Paul", "Donna", "Joshua", "Michelle",
	"Kenneth", "Carol", "Kevin", "Amanda", "Brian", "Melissa", "George", "Deborah", "Timothy", "Stephanie",
	"Ronald", "Dorothy", "Jason", "Rebecca", "Edward", "Sharon", "Jeffrey", "Laura", "Ryan", "Cynthia",
	"Jacob", "Amy", "Gary", "Kathleen", "Nicholas", "Angela", "Eric", "Shirley", "Jonathan", "Brenda",
	"Stephen", "Emma", "Larry", "Anna", "Justin", "Pamela", "Scott", "Nicole", "Brandon", "Samantha",
	"Benjamin", "Katherine", "Samuel", "Christine", "Gregory", "Helen", "Alexander", "Debra", "Patrick", "Rachel",
	"Frank", "Carolyn", "Raymond", "Janet", "Jack", "Maria", "Dennis", "Catherine", "Jerry", "Heather",
	"Tyler", "Diane", "Aaron", "Olivia", "Jose", "Julie", "Adam", "Joyce", "Nathan", "Victoria",
	"Henry", "Ruth", "Zachary", "Virginia", "Douglas", "Lauren", "Peter", "Kelly", "Kyle", "Christina",
	"Noah", "Joan", "Ethan", "Evelyn", "Jeremy", "Judith", "Walter", "Andrea", "Christian", "Hannah",
	"Keith", "Megan", "Roger", "Cheryl", "Terry", "Jacqueline", "Austin", "Martha", "Sean", "Madison",
	"Gerald", "Teresa", "Carl", "Gloria", "Harold", "Sara", "Dylan", "Janice", "Arthur", "Ann",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez",
	"Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin",
	"Lee", "Perez", "Thompson", "White", "Harris", "Sanchez", "Clark", "Ramirez", "Lewis", "Robinson",
	"Walker", "Young", "Allen", "King", "Wright", "Scott", "Torres", "Nguyen", "Hill", "Flores",
	"Green", "Adams", "Nelson", "Baker", "Hall", "Rivera", "Campbell", "Mitchell", "Carter", "Roberts",
	"Gomez", "Phillips", "Evans", "Turner", "Diaz", "Parker", "Cruz", "Edwards", "Collins", "Reyes",
	"Stewart", "Morris", "Morales", "Murphy", "Cook", "Rogers", "Gutierrez", "Ortiz", "Morgan", "Cooper",
	"Peterson", "Bailey", "Reed", "Kelly", "Howard", "Ramos", "Kim", "Cox", "Ward", "Richardson",
	"Watson", "Brooks", "Chavez", "Wood", "James", "Bennett", "Gray", "Mendoza", "Ruiz", "Hughes",
	"Price", "Alvarez", "Castillo", "Sanders", "Patel", "Myers", "Long", "Ross", "Foster", "Jimenez",
	"Powell", "Jenkins", "Perry", "Russell", "Sullivan", "Bell", "Coleman", "Butler", "Henderson", "Barnes",
	"Gonzales", "Fisher", "Vasquez", "Simmons", "Romero", "Jordan", "Patterson", "Alexander", "Hamilton", "Graham",
	"Reynolds", "Griffin", "Wallace", "Moreno", "West", "Cole", "Hayes", "Bryant", "Herrera", "Gibson",
	"Ellis", "Tran", "Medina", "Aguilar", "Stevens", "Murray", "Ford", "Castro", "Marshall", "Owens",
	"Harrison", "Fernandez", "McDonald", "Woods", "Washington", "Kennedy", "Wells", "Vargas", "Henry", "Chen",
}
