package gol

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

type Board struct {
	cells [][]uint8
	width int
	height int
}

/*
Create a board struct given a width and height
Note we create the columns first, so we need to do cells[y][x]
 */
func createBoard(width int, height int) *Board {
	cells := make([][]uint8, height)
	for x := range cells {
		cells[x] = make([]uint8, width)
	}
	return &Board{cells: cells, width: width, height: height}
}

func populateBoard(board Board, c distributorChannels) *Board {
	for y:=0; y<board.height; y++ {
		for x:=0; x<board.width; x++ {
			// board[r][c]
		}
	}
	return &board
}

/*
Here we add the width of the board to our x coordinate, and the height of our board to the y coordinate. This is in case
they are negative (since we will be subtracting from 0 sometimes). And we are %'ing them anyway, so it doesn't matter.
The % helps us when we would wrap around, e.g. if we want to check cell (0, 0)'s left-neighbour and our width is 256,
we have to check cell (255, 0)
 */
func getCellCount (board Board, x int, y int) uint8 {
	x = x + board.width
	x = x % board.width
	y = y + board.height
	y = y % board.height
	return board.cells[y][x]
}

func findNeighbours(board Board, x int, y int) int {
	neighbours := 0
	for i:=-1; i<=1; i++ {
		for j=-1; j<=1; j++ {
			if (getCellCount(board, x+j, y+i) > 0) && (i != 0) && (j != 0){
				neighbours++
			}
		}
	}
	return neighbours
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {

	board := createBoard(p.ImageWidth, p.ImageHeight)

	turn := 0


	// TODO: Execute all turns of the Game of Life.

	// TODO: Report the final state using FinalTurnCompleteEvent.


	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
