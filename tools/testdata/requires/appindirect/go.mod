module example.com/appindirect

go 1.21

require example.com/lib v0.1.0 // indirect

replace example.com/lib => ../lib
