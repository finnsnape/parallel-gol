package gol

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

// Board stores one game of life board and its width/height
type Board struct {
	cells [][]uint8
	width int
	height int
}

// BoardStates stores the current state of the board, the state of the board if we make one more turn, and the w/h
type BoardStates struct {
	current *Board
	next *Board
	width int
	height int
}

// Create a board struct given a width and height
// Note we create the columns first, so we need to do cells[y][x]
func createBoard(width int, height int) *Board {
	cells := make([][]uint8, height)
	for x := range cells {
		cells[x] = make([]uint8, width)
	}
	return &Board{cells: cells, width: width, height: height}
}

// Creates the board states
func createBoardStates(width int, height int, c distributorChannels) *BoardStates {
	current := createBoard(width, height)
	current.PopulateBoard(c) // set the cells of the current board to those from the input
	next := createBoard(width, height)
	return &BoardStates{current: current, next: next, width: width, height: height}
}

// PopulateBoard sets the board values to the input
func (board *Board) PopulateBoard(c distributorChannels) {
	for j:=0; j<board.height; j++ {
		for i:=0; i<board.width; i++ {
			board.Set(i, j, <- c.ioInput)
		}
	}
}

// Get gets the value from a cell
func (board *Board) Get (x int, y int) uint8 {
	return board.cells[y][x]
}

// Set sets the value of a cell
func (board *Board) Set (x int, y int, val uint8) {
	board.cells[y][x] = val
}

// CheckAlive checks if a cell is alive, accounting for wrap around
func (board *Board) CheckAlive (x int, y int) bool {
	x = x % board.width
	y = y % board.height
	return board.Get(x, y) == 255
}

// AdvanceCell returns the new value for a specific cell after a turn
func (board *Board) AdvanceCell(x int, y int) uint8 {
	neighbours := 0
	for i:=-1; i<=1; i++ {
		for j:=-1; j<=1; j++ {
			if board.CheckAlive(x+j, y+i) && (i != 0 && j != 0) { // ensure we aren't counting the cell itself
				neighbours++
			}
		}
	}
	if neighbours < 2 || neighbours > 3 {
		return 0 // dies
	} else if neighbours == 3 {
		return 255 // alive
	} else {
		return board.Get(x, y) // the same
	}

}

// AdvanceBoardStates moves the game forward by one turn
func (boardStates *BoardStates) AdvanceBoardStates() {
	current := boardStates.current
	next := boardStates.next
	height := boardStates.height
	width := boardStates.width
	for j:=0; j<height; j++ {
		for i:=0; i<width; i++ {
			next.Set(i, j, current.AdvanceCell(i, j))
		}
	}
	boardStates.current = next // now we have updated the board we make it the current board
	boardStates.next = createBoard(height, width) // make the next board a new empty board
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {

	boardStates := createBoardStates(p.ImageWidth, p.ImageHeight, c)

	turn := 0

	for i:=0; i<p.Turns; i++ {
		boardStates.AdvanceBoardStates()
		turn++
	}


	// TODO: Execute all turns of the Game of Life.

	// TODO: Report the final state using FinalTurnCompleteEvent.


	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
